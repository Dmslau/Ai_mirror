import logging
import subprocess
import requests
import json
import os
import dingtalk_stream
import asyncio
import sys
import time
from dingtalk_stream import AckMessage

# 日志配置
def setup_logger():
    logger = logging.getLogger()
    handler = logging.StreamHandler()
    handler.setFormatter(
        logging.Formatter('%(asctime)s %(levelname)-8s %(message)s [%(filename)s:%(lineno)d]')
    )
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    return logger

# 文件存储路径
CONVERSATION_FILE = "conversation_history.json"

# 模型列表
MODELS = {
    "普通": "deepseek",
    "深度思考": "deepseek-think",
    "联网搜索": "deepseek-search",
    "深度思考+联网搜索": "deepseek-think-search"
}

# 默认模型
DEFAULT_MODEL = "deepseek"

# 加载或初始化对话历史文件
def load_conversation_history():
    if os.path.exists(CONVERSATION_FILE):
        with open(CONVERSATION_FILE, "r", encoding="utf-8") as f:
            return json.load(f)
    return {"conversation_id": None, "model": DEFAULT_MODEL}

# 保存当前的会话ID和模型
def save_conversation_history(conversation_id, model):
    with open(CONVERSATION_FILE, "w", encoding="utf-8") as f:
        json.dump({"conversation_id": conversation_id, "model": model}, f, ensure_ascii=False, indent=2)

class CalcBotHandler(dingtalk_stream.ChatbotHandler):
    def __init__(self, logger: logging.Logger = None):
        super(dingtalk_stream.ChatbotHandler, self).__init__()
        if logger:
            self.logger = logger
        # 加载会话历史文件
        self.conversation_history = load_conversation_history()
        # 当前使用的 AI
        self.current_ai = "xy"

    async def process(self, callback: dingtalk_stream.CallbackMessage):
        try:
            self.logger.info(f"Received callback: {callback.data}")

            # 解析消息数据
            incoming_message = dingtalk_stream.ChatbotMessage.from_dict(callback.data)
            user_message = incoming_message.text.content.strip()
            self.logger.info(f"User message: {user_message}")

            # 切换AI
            if user_message == "切换AI":
                self.logger.info("Switching AI.")
                if self.current_ai == "xy":
                    self.current_ai = "ds"
                    response = "AI 已切换到深度思考（ds），现在支持重置和切换模型。"
                else:
                    self.current_ai = "xy"
                    response = "AI 已切换回 xy。只支持基础聊天，不支持重置和切换模型。"
                self.reply_text(response, incoming_message)
                return AckMessage.STATUS_OK, 'OK'

            # 处理xy模式下的请求（只支持基础聊天，不支持重置和切换模型）
            if self.current_ai == "xy":
                self.logger.info("Processing with xy AI.")
                if user_message == "重置" or user_message.startswith("切换模型"):
                    response = "xy不支持重置和切换模型功能。"
                    self.reply_text(response, incoming_message)
                    return AckMessage.STATUS_OK, 'OK'
                reply = await self.get_xy_reply(user_message)
                self.logger.info(f"xy reply: {reply}")
                self.reply_text(reply, incoming_message)
                return AckMessage.STATUS_OK, 'OK'

            # 处理ds模式下的请求（支持重置和切换模型）
            if self.current_ai == "ds":
                self.logger.info("Processing with deepseek AI.")
                if user_message == "重置":
                    # 清空会话ID，恢复默认模型
                    self.conversation_history["conversation_id"] = None
                    self.conversation_history["model"] = DEFAULT_MODEL
                    save_conversation_history(None, DEFAULT_MODEL)
                    response = "已开启新对话，模型已重置为默认（普通）。"
                    self.reply_text(response, incoming_message)
                    return AckMessage.STATUS_OK, 'OK'

                # 切换模型
                if user_message.startswith("切换模型"):
                    model_list = "\n".join([f"{name}: {model}" for name, model in MODELS.items()])
                    response = f"请选择要切换的模型：\n{model_list}"
                    self.reply_text(response, incoming_message)
                    return AckMessage.STATUS_OK, 'OK'

                if user_message in MODELS:
                    self.conversation_history["model"] = MODELS[user_message]
                    save_conversation_history(self.conversation_history["conversation_id"], MODELS[user_message])
                    response = f"切换完成，当前模型为：{user_message}"
                    self.reply_text(response, incoming_message)
                    return AckMessage.STATUS_OK, 'OK'

                ai_reply, new_conversation_id = await self.get_ds_reply(user_message)

                if new_conversation_id:
                    self.conversation_history["conversation_id"] = new_conversation_id
                    save_conversation_history(new_conversation_id, self.conversation_history["model"])

                self.logger.info(f"DS AI reply: {ai_reply}")
                self.reply_text(ai_reply, incoming_message)
                return AckMessage.STATUS_OK, 'OK'

        except Exception as e:
            self.logger.error(f"Error processing message: {str(e)}")
            return AckMessage.STATUS_FAILED, f"Error: {str(e)}"

    async def get_xy_reply(self, user_message):
        loop = asyncio.get_event_loop()
        self.logger.info(f"Getting reply from xy for message: {user_message}")
        return await loop.run_in_executor(None, self.run_xy, user_message)

    def run_xy(self, user_message):
        try:
            self.logger.info(f"Running xy subprocess with message: {user_message}")
            result = subprocess.run(
                ["./xy", user_message],  
                capture_output=True,
                text=True,
                check=True,
                timeout=60 
            )
            self.logger.info(f"xy output: {result.stdout.strip()}")
            return result.stdout.strip()
        except subprocess.TimeoutExpired:
            self.logger.error("xy request timed out.")
            return "请求超时，请稍后再试"
        except subprocess.CalledProcessError as e:
            self.logger.error(f"xy process error: {e.stderr}")
            return f"Error: {e.stderr}"
        except Exception as e:
            self.logger.error(f"Unexpected error in xy: {str(e)}")
            return f"Unexpected error: {str(e)}"

    # 调用 ds 的函数
    async def get_ds_reply(self, user_message):
        loop = asyncio.get_event_loop()
        self.logger.info(f"Getting reply from DS for message: {user_message}")
        return await loop.run_in_executor(None, self.run_ds, user_message)

    def run_ds(self, user_message):
        try:
            url = "http://:8000/v1/chat/completions"
            headers = {
                "Authorization": "",
                "Content-Type": "application/json"
            }
            payload = {
                "model": self.conversation_history.get("model", DEFAULT_MODEL),
                "messages": [{"role": "user", "content": user_message}],
                "stream": False
            }
            if self.conversation_history.get("conversation_id"):
                payload["conversation_id"] = self.conversation_history["conversation_id"]

            self.logger.info(f"Sending DS request with payload: {payload}")
            response = requests.post(url, headers=headers, json=payload)

            if response.status_code == 200:
                response_data = response.json()
                if "choices" in response_data and len(response_data["choices"]) > 0:
                    ai_reply = response_data['choices'][0]['message']['content']
                    new_conversation_id = response_data.get('id')
                    self.logger.info(f"DS response: {ai_reply}")
                    return ai_reply, new_conversation_id
                else:
                    self.logger.error("DS response missing 'choices' or empty.")
                    return "出错了，请稍后再试。", None
            else:
                self.logger.error(f"DS request failed with status code: {response.status_code}")
                return "请求失败，请稍后再试。", None
        except Exception as e:
            self.logger.error(f"Error in DS API request: {str(e)}")
            return "请求失败，请稍后再试。", None


# 启动钉钉 Stream 客户端
def main():
    logger = setup_logger()

    # 钉钉授权凭证
    client_id = ""
    client_secret = ""

    credential = dingtalk_stream.Credential(client_id, client_secret)
    client = dingtalk_stream.DingTalkStreamClient(credential)
    client.register_callback_handler(dingtalk_stream.chatbot.ChatbotMessage.TOPIC, CalcBotHandler(logger))
    client.start_forever()


if __name__ == '__main__':
    main()
