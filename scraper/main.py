import os
import asyncio
import json
from crawl4ai import AsyncWebCrawler, BrowserConfig, CrawlerRunConfig, CacheMode, LLMConfig
from crawl4ai.extraction_strategy import LLMExtractionStrategy
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from dotenv import load_dotenv
from websockets.asyncio.server import serve


async def serve_websocket(websocket):
    cron_trigger = CronTrigger.from_crontab(os.environ["CRONTAB"])
    scheduler.add_job(run_scraping_task,
                      trigger=cron_trigger, args=[websocket])


"""
The websocket connection for sending scraped results back to the client
"""


async def run_scraping_task(websocket):
    browser_config = BrowserConfig(verbose=True)
    run_config = CrawlerRunConfig(
        word_count_threshold=1,
        extraction_strategy=LLMExtractionStrategy(
            llm_config=LLMConfig(
                provider="gemini/gemini-2.0-flash",
                api_token=os.environ["GEMINI_API_KEY"]
            ),
            schema=json.loads(os.environ["SCHEMA"]),
            extraction_type="schema",
            instruction=os.environ["PROMPT"]
        ),
        cache_mode=CacheMode.BYPASS
    )

    async with AsyncWebCrawler(config=browser_config) as crawler:
        result = await crawler.arun(
            url=os.environ["WEBPAGE_URL"],
            config=run_config
        )
        await websocket.send(result[0].extracted_content)
        print(result[0].extracted_content)


load_dotenv()

scheduler = BackgroundScheduler()


async def main():
    async with serve(serve_websocket, "localhost", 8765) as server:
        await server.serve_forever()

if __name__ == "__main__":
    asyncio.run(main())
