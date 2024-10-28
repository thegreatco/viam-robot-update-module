import asyncio
import datetime
import json
import os
import ssl
from typing import Any, ClassVar, Dict, List, Mapping, Optional, Sequence, Tuple
from grpclib.client import Channel

from typing_extensions import Self

from viam.app.viam_client import ViamClient, AppClient
from viam.app.app_client import RobotPart
from viam.rpc.dial import AuthenticatedChannel, DialOptions, Credentials, _get_access_token
from viam.robot.client import RobotClient
from viam.components.generic import Generic
from viam.logging import getLogger
from viam.module.module import Module
from viam.proto.app.robot import ComponentConfig
from viam.proto.common import ResourceName
from viam.resource.base import ResourceBase
from viam.resource.registry import Registry, ResourceCreatorRegistration
from viam.resource.types import Model, ModelFamily
from viam.utils import ValueTypes

LOGGER = getLogger(__name__)
Namespace = "myNamespace"

def conf_to_dict(conf: Mapping[str,Any]) -> Dict[str, Any]:
    return dict(conf)
   
def get_machine_config() -> Mapping[str, Any]:
    filePath = os.getenv("VIAM_CONFIG_FILE")
    if filePath is None or filePath == "" or not os.path.exists(filePath):
        filePath = "/root/.viam/"
        files = os.listdir(filePath)
        matching_files = [file for file in files if file.startswith("cached_cloud_config_") and file.endswith(".json")]
        if len(matching_files) == 0:
            raise ValueError("no matching file found")
        filePath = os.path.join('/root/.viam', matching_files[0])
    with open(filePath, 'r') as file:
        data = json.load(file)
        if "cloud" not in data:
            raise ValueError("machine not found in config file")
        return data
    
def get_machine_part_id() -> str:
    config = get_machine_config()
    if "cloud" not in config or "id" not in config["cloud"]:
        raise ValueError("machine or id not found in config file")
    return config["cloud"]["id"]

def get_machine_id() -> str:
    config = get_machine_config()
    if "cloud" not in config or "machine_id" not in config["cloud"]:
        raise ValueError("machine or part_id not found in config file")
    return config["cloud"]["machine_id"]

def get_machine_fqdn() -> str:
    config = get_machine_config()
    if "cloud" not in config or "fqdn" not in config["cloud"]:
        raise ValueError("fqdn not found in config file")
    return config["cloud"]["fqdn"]

def getCredentialsFromConfig() -> Tuple[str, str]:
    config = get_machine_config()
    if "auth" not in config:
        raise ValueError("auth not found in config file")
    auth = config["auth"]
    if "handlers" not in auth:
        raise ValueError("handlers not found in config file")
    handlers = auth["handlers"]
    if len(handlers) == 0:
        raise ValueError("no handlers found in config file")
    handler = [handler for handler in handlers if handler["type"] == "api-key"][0]
    return handler["config"]["keys"][0], handler["config"][handler["config"]["keys"][0]]

async def getAppClient(command: Mapping[str, ValueTypes]) -> Optional[AppClient]:
    client: Optional[AppClient] = None
    if "apiKeyName" not in command or "apiKey" not in command:
        LOGGER.debug("no API key provided, trying to use machine credentials")
        try:
            cloudId, cloudSecret = getCredentialsFromConfig()
            client = await getAppClientFromApiCredentials(cloudId, cloudSecret)
        except Exception as e:
            LOGGER.error(f"error getting client: {e}")
    else:
        LOGGER.debug("API key provided, using it")
        client = await getAppClientFromApiCredentials(str(command["apiKeyName"]), str(command["apiKey"]))
    
    return client

async def getRobotClient(command: Mapping[str, ValueTypes]) -> Optional[RobotClient]:
    client: Optional[RobotClient] = None
    if "apiKeyName" not in command or "apiKey" not in command:
        LOGGER.debug("no API key provided, trying to use machine credentials")
        try:
            cloudId, cloudSecret = getCredentialsFromConfig()
            client = await getRobotClientFromApiCredentials(cloudId, cloudSecret, get_machine_fqdn())
        except Exception as e:
            LOGGER.error(f"error getting client: {e}")
    else:
        LOGGER.debug("API key provided, using it")
        client = await getRobotClientFromApiCredentials(str(command["apiKeyName"]), str(command["apiKey"]), get_machine_fqdn())
    
    return client

async def dial_app(address: str, options: DialOptions) -> Channel:
    ctx = ssl.create_default_context(purpose=ssl.Purpose.SERVER_AUTH)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    ctx.set_ciphers("ECDHE+AESGCM:ECDHE+CHACHA20:DHE+AESGCM:DHE+CHACHA20")
    ctx.set_alpn_protocols(["h2"])

    channel = AuthenticatedChannel(address, 443, ssl=ctx)
    access_token = await _get_access_token(channel, address, options)
    metadata = {"authorization": f"Bearer {access_token}"}
    channel._metadata = metadata

    return channel

async def getAppClientFromApiCredentials(apiKeyName:str, apiKey:str) -> AppClient:
    dial_options = DialOptions.with_api_key(apiKey, apiKeyName)
    client = await ViamClient.create_from_dial_options(dial_options)
    return client.app_client

async def getRobotClientFromApiCredentials(apiKeyName:str, apiKey:str, address:str) -> RobotClient:
    opts = RobotClient.Options.with_api_key(
        api_key=apiKey,
        api_key_id=apiKeyName
    )
    return await RobotClient.at_address(address, opts)

def swap_fragment_id(oldFragmentId: str, newFragmentId: str, conf:Dict[str, Any]) -> None:
    # Get the fragments (or an empty array if fragments is not found)
    fragments = conf.get("fragments", [])

    if not isinstance(fragments, list):
        LOGGER.error(f"invalid fragments: {fragments}")
        return

    # Filter out the old fragmentId, we also do the new fragmentId to prevent duplicates, just in case
    filteredFragments = list(filter(lambda x: x != oldFragmentId and x != newFragmentId, fragments))
    # Log the fragments found, this is mostly for debugging
    for fragment in fragments:
        LOGGER.info(f"found fragment: {fragment}")

    filteredFragments.append(newFragmentId)

    # Set the fragments to an array of just the fragmentId
    conf["fragments"] = filteredFragments

    for mod in conf.get("fragment_mods", []):
        if mod.get("fragment_id", "") == oldFragmentId:
            mod["fragment_id"] = newFragmentId

class UpdateModule(Generic):
    MODEL: ClassVar[Model] = Model(ModelFamily(Namespace, "machine"), "update")

    def __init__(self, name: str):
        super().__init__(name)

    @classmethod
    def new(cls, config: ComponentConfig, dependencies: Mapping[ResourceName, ResourceBase]) -> Self:
        sensor = cls(config.name)
        sensor.reconfigure(config, dependencies)
        return sensor

    @classmethod
    def validate_config(cls, config: ComponentConfig) -> Sequence[str]:
        return []

    async def do_command(self, command: Mapping[str, ValueTypes], *, timeout: Optional[float] = None, **kwargs) -> Mapping[str, ValueTypes]:
        if "command" in command:
            cmd = command["command"]
            if cmd == "update":
                LOGGER.info(f"update command received: {command}")
                
                newFragmentId = command.get("newFragmentId", "")
                if newFragmentId == "" or newFragmentId is None or not isinstance(newFragmentId, str):
                    return {"error": "newFragmentId missing or invalid"}
                oldFragmentId = command.get("oldFragmentId", "")
                if oldFragmentId == "" or oldFragmentId is None or not isinstance(oldFragmentId, str):
                    return {"error": "oldFragmentId missing or invalid"}
                app_client = await getAppClient(command)                
                if app_client is None:
                    return {"error": "credentials not found"}
                machineId = get_machine_id()
                LOGGER.debug(f"client created, updating configuration for machine {machineId} with fragment {newFragmentId}")
                await self.updateFragment(app_client, machineId, oldFragmentId, newFragmentId)
                
                if app_client is not None and app_client._channel is not None:
                    app_client._channel.close()
                return {"ok": 1}
            elif cmd == "restart":
                LOGGER.info("restart requested")
                app_client = await getAppClient(command)
                if app_client is None:
                    return {"error": "credentials not found"}
                machine_part_id = get_machine_part_id()
                await app_client.mark_part_for_restart(machine_part_id)
                LOGGER.info("sent restart request")
                return {"ok": 1}
            elif cmd == "restart_on_rdk_update":
                LOGGER.info("restart on update requested")
                version = command.get("version", "")
                if version == "" :
                    return {"error": "no version provided"}
                
                app_client = await getAppClient(command)
                if app_client is None:
                    return {"error": "credentials not found"}
                machine_part_id = get_machine_part_id()
                robot_client = await getRobotClient(command)
                if robot_client is None:
                    return {"error": "credentials not found"}
                status = await robot_client.get_machine_status()
                await app_client.mark_part_for_restart(machine_part_id)
                LOGGER.info("sent restart on update request")
                return {"ok": 1}
                
        return {"error": "no command provided"}
    
    async def updateFragment(self, client: AppClient, machineId: str, oldFragmentId: str, newFragmentId: str) -> Mapping[str, ValueTypes]:
        try:
            machine = await client.get_robot(machineId)
            if machine is None:
                return {"error": "machine not found"}
            if machine.last_access is None or machine.last_access.ToDatetime() < datetime.datetime.now() - datetime.timedelta(minutes=1):
                return {"error": "machine not accessed in the last 60 seconds"}
            LOGGER.debug(f"machine found: {machine}")

            try:
                part = await self.get_machine_part(client, machineId)

                LOGGER.debug(f"machine part found: {part}")

                # Get the machine configuration
                conf = part.robot_config

                if conf is None:
                    return {"error": "no configuration found for machine part"}

                # Swap the fragmentId
                swap_fragment_id(oldFragmentId, newFragmentId, conf_to_dict(conf))
                
                LOGGER.debug(f"new configuration: {conf}")
                try:
                    # Update the machine part with the new configuration
                    await client.update_robot_part(part.id, part.name, conf)
                except Exception as e:
                    LOGGER.error(f"error updating machine part: {e}")
                    return {"error": f"error updating machine part: {e}"}
                return {"ok": 1}
            except Exception as e:
                LOGGER.error(f"error getting machine parts: {e}")
                return {"error": f"error getting machine parts: {e}"}
        except Exception as e:
            LOGGER.error(f"error getting machine: {e}")
            return {"error": f"error getting machine: {e}"}

    def reconfigure(self, config: ComponentConfig, dependencies: Mapping[ResourceName, ResourceBase]):
        pass

    async def close(self):
        LOGGER.info(f"{self.name} is closed.")

    async def get_machine_part(self, client: AppClient, machineId: str) -> RobotPart:
        parts = await client.get_robot_parts(machineId)
        if parts is None or len(parts) == 0:
            raise Exception("no parts found for machine")
        if len(parts) > 1:
            raise Exception("more than one part found for machine")
        
        # Get the first part
        return parts[0]

async def main():
    """This function creates and starts a new module, after adding all desired resource models.
    Resource creators must be registered to the resource registry before the module adds the resource model.
    """
    Registry.register_resource_creator(Generic.SUBTYPE, UpdateModule.MODEL, ResourceCreatorRegistration(UpdateModule.new, UpdateModule.validate_config))

    module = Module.from_args()
    module.add_model_from_registry(Generic.SUBTYPE, UpdateModule.MODEL)
    await module.start()

if __name__ == "__main__":
    asyncio.run(main())
