import asyncio
import datetime
import json
import os
import ssl
from typing import Any, ClassVar, Dict, Mapping, Optional, Sequence, Tuple
from grpclib.client import Channel

from typing_extensions import Self

from viam.app.viam_client import ViamClient, AppClient
from viam.rpc.dial import AuthenticatedChannel, DialOptions, Credentials, _get_access_token
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
Namespace = "tennibot"

def getCredentialsFromConfig() -> Tuple[str, str]:
    filePath = os.getenv("VIAM_CONFIG_FILE")
    if filePath is None or filePath == "" or not os.path.exists(filePath):
        filePath = "/etc/viam.json"
    with open(filePath, 'r') as file:
        data = json.load(file)
        if "cloud" not in data:
            raise ValueError("cloud not found in config file")
        cloud = data["cloud"]
        if "id" not in cloud or "secret" not in cloud:
            raise ValueError("id or secret not found in cloud")
        return cloud["id"], cloud["secret"]
    
async def getAppClientFromConfigCredentials(cloudId, cloudSecret) -> AppClient:
    dial_options = DialOptions(disable_webrtc=True, credentials=Credentials(type="robot-secret", payload=cloudSecret), auth_entity=cloudId)
    channel = await dial_app("app.viam.com", dial_options)
    return AppClient(channel, {})

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

async def getAppClientFromApiCredentials(apiKeyName, apiKey) -> AppClient:
    dial_options = DialOptions.with_api_key(apiKey, apiKeyName)
    client = await ViamClient.create_from_dial_options(dial_options)
    return client.app_client

def swap_fragment_id(oldFragmentId: str, newFragmentId: str, conf:Mapping[str, ValueTypes]) -> None:
    # Get the fragments (or an empty array if fragments is not found)
    fragments = conf.get("fragments", [])

    # Filter out the old fragmentId, we also do the new fragmentId to prevent duplicates, just in case
    filteredFragments = list(filter(lambda x: x != oldFragmentId and x != newFragmentId, fragments))
    # Log the fragments found, this is mostly for debugging
    for fragment in fragments:
        LOGGER.info(f"Found fragment: {fragment}")

    filteredFragments.append(newFragmentId)

    # Set the fragments to an array of just the fragmentId
    conf["fragments"] = filteredFragments

    for mod in conf.get("fragment_mods", []):
        if mod.get("fragment_id", "") == oldFragmentId:
            mod["fragment_id"] = newFragmentId

class UpdateModule(Generic):
    MODEL: ClassVar[Model] = Model(ModelFamily(Namespace, "robot"), "update")

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
                LOGGER.info(f"Update command received: {command}")
                newFragmentId = command.get("newFragmentId", "")
                if newFragmentId == "":
                    return {"error": "newFragmentId missing"}
                oldFragmentId = command.get("oldFragmentId", "")
                if oldFragmentId == "":
                    return {"error": "oldFragmentId missing"}
                robotId = command.get("robotId", "")
                if robotId == "":
                    return {"error": "robotId missing"}

                client: AppClient = None
                if "apiKeyName" not in command or "apiKey" not in command:
                    LOGGER.debug("No API key provided, trying to use robot credentials")
                    try:
                        cloudId, cloudSecret = getCredentialsFromConfig()
                        client = await getAppClientFromConfigCredentials(cloudId, cloudSecret)
                    except Exception as e:
                        LOGGER.error(f"Error getting client: {e}")
                else:
                    LOGGER.debug("API key provided, using it")
                    client = await getAppClientFromApiCredentials(command["apiKeyName"], command["apiKey"])
                
                if client is None:
                    return {"error": "credentials not found"}
                LOGGER.debug(f"Client created, updating configuration for robot {robotId} with fragment {newFragmentId}")
                await self.updateFragment(client, robotId, oldFragmentId, newFragmentId)
                
                if client is not None and client._channel is not None:
                    client._channel.close()
                return {"ok": 1}
        return {"error": "no command provided"}
    
    async def updateFragment(self, client: AppClient, robotId: str, oldFragmentId: str, newFragmentId: str) -> Mapping[str, ValueTypes]:
        try:
            robot = await client.get_robot(robotId)
            if robot is None:
                return {"error": "Robot not found"}
            if robot.last_access is None or robot.last_access.ToDatetime() < datetime.datetime.now() - datetime.timedelta(minutes=1):
                return {"error": "Robot not accessed in the last 60 seconds"}
            LOGGER.debug(f"Robot found: {robot}")

            try:
                parts = await client.get_robot_parts(robotId)
                if parts is None or len(parts) == 0:
                    return {"error": "No parts found for robot"}
                if len(parts) > 1:
                    return {"error": f"More than one part found for robot: {robotId}"}
                
                # Get the first part
                part = parts[0]

                LOGGER.debug(f"Robot part found: {part}")

                # Get the robot configuration
                conf = part.robot_config

                # Swap the fragmentId
                swap_fragment_id(oldFragmentId, newFragmentId, conf)
                
                LOGGER.debug(f"New configuration: {conf}")
                try:
                    # Update the robot part with the new configuration
                    await client.update_robot_part(part.id, part.name, conf)
                except Exception as e:
                    LOGGER.error(f"Error updating robot part: {e}")
                    return {"error": f"Error updating robot part: {e}"}
                return {"ok": 1}
            except Exception as e:
                LOGGER.error(f"Error getting robot parts: {e}")
                return {"error": f"Error getting robot parts: {e}"}
        except Exception as e:
            LOGGER.error(f"Error getting robot: {e}")
            return {"error": f"Error getting robot: {e}"}

    def reconfigure(self, config: ComponentConfig, dependencies: Mapping[ResourceName, ResourceBase]):
        pass

    async def close(self):
        LOGGER.info(f"{self.name} is closed.")

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
