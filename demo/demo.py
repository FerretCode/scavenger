import os
import asyncio
import websockets
import json
from dotenv import load_dotenv
from datetime import datetime

load_dotenv()


async def websocket_client():
    uri = f"{os.environ['SCAVENGER_WEBSOCKET_SERVER']
             }/connect/{os.environ['SCAVENGER_WORKFLOW_NAME']}"
    headers = {
        "X-API-Key": os.environ['SCAVENGER_API_KEY'],
    }

    async with websockets.connect(uri, additional_headers=headers) as websocket:
        try:
            while True:
                # Receive a message from the server
                response = await websocket.recv()
                data = json.loads(response)
                current_time = datetime.now().strftime("%H:%M:%S")
                debt = data[0].get("debt", None)
                print(f"[{current_time}] ${debt:,}")

        except websockets.ConnectionClosed:
            print("Connection closed")

print("Debt Tracker:")
asyncio.run(websocket_client())
