import asyncio
import datetime
import json
import os
from typing import Any, ClassVar, Dict, Mapping, Optional, Sequence

from typing_extensions import Self

from viam.app.viam_client import ViamClient, AppClient
from viam.rpc.dial import DialOptions, Credentials
from viam.components.generic import Generic
from viam.logging import getLogger
from viam.module.module import Module
from viam.proto.app.robot import ComponentConfig
from viam.proto.common import ResourceName
from viam.resource.base import ResourceBase
from viam.resource.registry import Registry, ResourceCreatorRegistration
from viam.resource.types import Model, ModelFamily
from viam.utils import SensorReading, ValueTypes

LOGGER = getLogger(__name__)
Namespace = "tennibot"

def getCredentialsFromConfig():
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

def getAppClientFromConfigCredentials(cloudId, cloudSecret):
    dial_options = DialOptions(credentials=Credentials(type="robot-secret", payload=cloudSecret), auth_entity=cloudId)
    return ViamClient.create_from_dial_options(dial_options)

def getAppClientFromApiCredentials(apiKeyName, apiKey):
    dial_options = DialOptions.with_api_key(apiKey, apiKeyName)
    return ViamClient.create_from_dial_options(dial_options)

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
                if "fragmentId" not in command:
                    return {"error": "No fragmentId provided"}
                fragmentId = command["fragmentId"]
                if "robotId" not in command:
                    return {"error": "No robotId provided"}
                robotId = command["robotId"]

                client: ViamClient = None
                if "apiKeyName" not in command or "apiKey" not in command:
                    cloudId, cloudSecret = getCredentialsFromConfig()
                    client = getAppClientFromConfigCredentials(cloudId, cloudSecret)
                else:
                    client = getAppClientFromApiCredentials(command["apiKeyName"], command["apiKey"])
                
                if client is None:
                    return {"error": "No client created"}
                
                self.updateFragment(client.app_client, robotId, fragmentId)
        return command
    
    async def updateFragment(self, client: AppClient, robotId: str, fragmentId: str) -> Mapping[str, ValueTypes]:
        try:
            robot = await client.get_robot(robotId)
            if robot is None:
                return {"error": "Robot not found"}
            if robot.last_access is None or robot.last_access < datetime.now() - datetime.timedelta(minutes=1):
                return {"error": "Robot not accessed in the last 60 seconds"}
            try:
                parts = await client.get_robot_parts(robotId)
                if parts is None or len(parts) == 0:
                    return {"error": "No parts found for robot"}
                if len(parts) > 1:
                    return {"error": f"More than one part found for robot: {robotId}"}
                
                part = parts[0]
                conf = part.robot_config
                fragments = conf.get("fragments", [])
                for fragment in fragments:
                    LOGGER.info(f"Found fragment: {fragment}")
                fragments = [fragmentId]
                try:
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
