import base64
import copy
import json
import os
import re
import time
import urllib.request

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
with open(os.environ["GATEWAY_SEMANTIC_CASES"], encoding="utf-8") as source:
    cases = json.load(source)
clients = {}
for name, variable in (("safe", "GATEWAY_SDK_KEY"), ("strict", "GATEWAY_SEMANTIC_STRICT_KEY"), ("foreign", "GATEWAY_SDK_FOREIGN_KEY")):
    key = os.environ[variable]
    clients[name] = (
        OpenAI(base_url=base + "/v1", api_key=key, max_retries=0),
        Anthropic(base_url=base, api_key=key, max_retries=0),
        genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta")),
    )
state = {}
counts = {}


def observe():
    with urllib.request.urlopen(os.environ["GATEWAY_SEMANTIC_OBSERVER"], timeout=3) as response:
        return json.load(response)


def quota_after_charge(value, charge):
    assert isinstance(value, str) and re.fullmatch(r"0|-?[1-9]\d*", value), "quota must use the decimal string wire contract"
    return str(int(value) - charge)


def request_for(test):
    request = copy.deepcopy(test["body"])
    protocol, feature, action = test["protocol"], test["feature"], test["action"]
    if protocol == "gemini" and feature == "vision":
        parts = request["contents"][0]["parts"]
        image = parts[1]["inlineData"]
        request["contents"] = [types.Content(role="user", parts=[types.Part(text=test["label"]), types.Part.from_bytes(data=base64.b64decode(image["data"]), mime_type=image["mimeType"])])]
    if feature not in ("state", "private_file") or action == "seed":
        return request
    unknown = action == "unknown"
    if protocol == "chat":
        request["messages"] = [{"role": "user", "content": [{"type": "text", "text": test["label"]}, {"type": "file", "file": {"file_id": "file-unknown" if unknown else "file-semantic-local"}}]}]
    elif protocol == "responses":
        item = state[(protocol, feature)].output[0]
        if unknown:
            item = item.model_copy(update={"encrypted_content": "semantic-unknown", "id": "reason-unknown"})
        request["input"] = [item, {"role": "user", "content": test["label"]}]
    elif protocol == "anthropic":
        block = state[(protocol, feature)].content[0]
        if unknown:
            block = block.model_copy(update={"signature": "unknown-signature"})
        request["messages"] = [{"role": "assistant", "content": [block]}, {"role": "user", "content": test["label"]}]
    else:
        part = state[(protocol, feature)].candidates[0].content.parts[0].model_copy(deep=True)
        if unknown:
            if feature == "private_file":
                part.file_data.file_uri = "https://generativelanguage.googleapis.com/v1beta/files/unknown-semantic"
            else:
                part.thought_signature = b"unknown-signature"
        request["contents"] = [types.Content(role="model", parts=[part]), types.Content(role="user", parts=[types.Part(text=test["label"])])]
    return request


def call(test):
    oai, claude, gemini = clients["foreign" if test["foreign"] else test["policy"]]
    request = request_for(test)
    protocol = test["protocol"]
    if protocol == "chat":
        if test["stream"]:
            with oai.chat.completions.stream(**request) as stream:
                response = stream.get_final_completion()
        else:
            response = oai.chat.completions.create(**request)
        assert response.choices[0].finish_reason == "stop"
        text = response.choices[0].message.content
        usage = (response.usage.prompt_tokens, response.usage.completion_tokens)
    elif protocol == "responses":
        if test["stream"]:
            with oai.responses.stream(**request) as stream:
                response = stream.get_final_response()
        else:
            response = oai.responses.create(**request)
        assert response.status == "completed"
        text = response.output_text
        usage = (response.usage.input_tokens, response.usage.output_tokens)
    elif protocol == "anthropic":
        if test["stream"]:
            with claude.messages.stream(**request) as stream:
                response = stream.get_final_message()
        else:
            response = claude.messages.create(**request)
        assert response.stop_reason == "end_turn"
        text = "".join(block.text for block in response.content if block.type == "text")
        usage = (response.usage.input_tokens, response.usage.output_tokens)
    else:
        request["config"] = types.GenerateContentConfig(**request["config"], automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True))
        if test["stream"]:
            chunks = list(gemini.models.generate_content_stream(**request))
            text, response = "".join(chunk.text or "" for chunk in chunks), chunks[-1]
        else:
            response = gemini.models.generate_content(**request)
            text = response.text
        assert response.candidates[0].finish_reason == "STOP"
        usage = (response.usage_metadata.prompt_token_count, response.usage_metadata.candidates_token_count)
    assert usage == (10, 2), usage
    if test["feature"] == "structured":
        assert json.loads(text) == {"value": "hello"}, text
    else:
        assert text == "hello", text
    if test["action"] == "seed":
        state[(protocol, test["feature"])] = response


for test in cases:
    before = observe()
    try:
        call(test)
    except Exception as error:
        status = getattr(error, "status_code", getattr(error, "code", None))
        assert test["reject"] and status == 400, (test["label"], type(error).__name__, str(error))
    else:
        assert not test["reject"], f"{test['label']}: rejected semantic request succeeded"
    charge = 0 if test["reject"] else 500
    expected = {"sends": before["sends"] + int(not test["reject"]), "quota": quota_after_charge(before["quota"], charge), "token": quota_after_charge(before["token"], charge), "logs": before["logs"] + int(not test["reject"]), "billing_sessions": before["billing_sessions"] + int(not test["reject"])}
    deadline = time.monotonic() + 2
    after = observe()
    while after != expected and time.monotonic() < deadline:
        time.sleep(0.01)
        after = observe()
    assert after == expected, (test["label"], before, after, expected)
    count = counts.setdefault(test["protocol"], {"success": 0, "rejected": 0})
    count["rejected" if test["reject"] else "success"] += 1
    print(f"SEMANTIC_PATH_OK: {test['label']}", flush=True)
print("SEMANTIC_SDK_OK: Python " + json.dumps(counts, sort_keys=True))
