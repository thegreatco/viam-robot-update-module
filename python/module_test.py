from typing import Any, List, Mapping, Optional
from datetime import datetime, timezone
import unittest
import json
import os
from module import UpdateModule, swap_fragment_id

from unittest.mock import AsyncMock, create_autospec
from viam.utils import ValueTypes
from viam.app.viam_client import AppClient
from viam.proto.app import Robot, RobotPart, GetRobotResponse, GetRobotPartsResponse
from viam.app.app_client import RobotPart, Robot
from google.protobuf import json_format

def load_json(file_path) -> Mapping[str, Any]:
    with open(file_path, 'r') as json_file:
        data = json.load(json_file)
    return data

def get_robot() -> Robot:
    with open("testdata/GetRobotResponse.json", 'r') as json_file:
        f = json_file.read()
        f = f.replace("$ONLINEDATE", datetime.now().replace(tzinfo=timezone.utc).isoformat(timespec='seconds'))
        r = GetRobotResponse()
        json_format.Parse(f, r)
        return r.robot
    
def get_robot_parts() -> List[RobotPart]:
    with open("testdata/GetRobotPartsResponse.json", 'r') as json_file:
        f = json_file.read()
        r = GetRobotPartsResponse()
        json_format.Parse(f, r)
        return [RobotPart.from_proto(robot_part=part) for part in r.parts]

class TestModule(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.maxDiff = None
        self.module = UpdateModule("testModule")

    def test_swap_fragment_id(self):
        part = load_json("testdata/robot_part_config.json")
        conf = part["robotConfig"]
        swap_fragment_id("abf95d7c-424a-49f2-b861-9ce999eac2fa", "6abb7bab-769c-4a31-a13b-0f7efa7ab670", conf)
        updated_part = json.dumps(part, sort_keys=True)
        expected_updated_part = json.dumps(load_json("testdata/robot_part_config_updated.json"), sort_keys=True)

        self.assertEqual(updated_part, expected_updated_part)
        
    async def test_do_command_errors(self):
        command = {}
        o = await self.module.do_command(command)
        self.assertEqual(o["error"], "no command provided")

        command["command"] = "update"
        o = await self.module.do_command(command)
        self.assertEqual(o["error"], "newFragmentId missing or invalid")

        command["newFragmentId"] = "6abb7bab-769c-4a31-a13b-0f7efa7ab670"
        o = await self.module.do_command(command)
        self.assertEqual(o["error"], "oldFragmentId missing or invalid")

        command["oldFragmentId"] = "abf95d7c-424a-49f2-b861-9ce999eac2fa"
        o = await self.module.do_command(command)
        self.assertEqual(o["error"], "robotId missing or invalid")

        os.environ["VIAM_CONFIG_FILE"] = "testdata/viam.json"
        command["robotId"] = "robotId"
        o = await self.module.do_command(command)
        self.assertEqual(o["error"], "credentials not found")

    async def test_update_fragment(self):
        mockClient = create_autospec(AppClient, instance=True)
        mockClient.get_robot = AsyncMock(return_value=get_robot())
        mockClient.get_robot_parts = AsyncMock(return_value=get_robot_parts())
        mockClient.update_robot_part = AsyncMock(return_value=RobotPart())
        res = await self.module.updateFragment(mockClient, "3bf2974e-59af-409c-bed1-afc1c73d029b", "abf95d7c-424a-49f2-b861-9ce999eac2fa", "6abb7bab-769c-4a31-a13b-0f7efa7ab670")
        if "error" in res:
            self.fail(res["error"])

if __name__ == '__main__':
    unittest.main()
