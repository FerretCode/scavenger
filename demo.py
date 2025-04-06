import asyncio
import websockets
import json
from datetime import datetime

async def websocket_client():
    uri = "ws://127.0.0.1:3000/connect/debt"
    headers = {
        "X-API-Key": "bb2c1ab22cda6466b686b1ab2e35cb63570163e6e063299cfb438ef1226d3318",
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
