import os

from anthropic import Anthropic
from google import genai
from google.genai import types
from openai import OpenAI


base = os.environ["GATEWAY_SDK_URL"]
key = os.environ["GATEWAY_SDK_KEY"]
oai = OpenAI(base_url=base + "/v1", api_key=key, max_retries=0)
claude = Anthropic(base_url=base, api_key=key, max_retries=0)
gemini = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base, api_version="v1beta"))
failures = []
successes = rejected = interrupted = 0
for provider in ("baidu", "cohere", "tencent", "zhipu", "dify", "palm"):
    model = "legacy-" + provider
    for mode in ("json", "stream"):
        label = f"legacy-{provider}-chat-{mode}"
        try:
            request = {"model": model, "messages": [{"role": "user", "content": label}]}
            if mode == "stream":
                with oai.chat.completions.stream(**request) as stream:
                    for event in stream:
                        if event.type == "chunk":
                            assert all(choice.index == 0 for choice in event.chunk.choices), event.chunk
                    response = stream.get_final_completion()
            else:
                response = oai.chat.completions.create(**request)
            assert len(response.choices) == 1, response.choices
            assert response.choices[0].message.role == "assistant", response.choices[0].message.role
            assert response.choices[0].message.content == "hello", response.choices[0].message.content
            assert response.choices[0].finish_reason == "stop", response.choices[0].finish_reason
            successes += 1
            print(f"LEGACY_PATH_OK: {label}")
        except Exception as error:
            failures.append(f"{label}: {type(error).__name__}: {error}")
    for protocol in ("responses", "anthropic", "gemini"):
        for mode in ("json", "stream"):
            label = f"legacy-{provider}-{protocol}-{mode}"
            try:
                if protocol == "responses":
                    request = {"model": model, "input": label, "max_output_tokens": 32}
                    if mode == "stream":
                        with oai.responses.stream(**request) as stream:
                            stream.get_final_response()
                    else:
                        oai.responses.create(**request)
                elif protocol == "anthropic":
                    request = {"model": model, "messages": [{"role": "user", "content": label}], "max_tokens": 32}
                    if mode == "stream":
                        with claude.messages.stream(**request) as stream:
                            stream.get_final_message()
                    else:
                        claude.messages.create(**request)
                else:
                    request = {"model": model, "contents": label, "config": types.GenerateContentConfig(max_output_tokens=32)}
                    if mode == "stream":
                        list(gemini.models.generate_content_stream(**request))
                    else:
                        gemini.models.generate_content(**request)
                raise AssertionError("unregistered protocol was accepted")
            except Exception as error:
                status = getattr(error, "status_code", None) or getattr(error, "code", None)
                if status == 503:
                    rejected += 1
                    print(f"LEGACY_PATH_REJECTED: {label}")
                else:
                    failures.append(f"{label}: {type(error).__name__}: {error}")
    label = f"legacy-{provider}-chat-interrupt"
    delivered, terminal, failed = "", False, False
    try:
        with oai.chat.completions.stream(model=model, messages=[{"role": "user", "content": label}]) as stream:
            for event in stream:
                if event.type == "content.delta":
                    delivered += event.delta
                if event.type == "chunk":
                    terminal = terminal or any(choice.finish_reason for choice in event.chunk.choices)
            stream.get_final_completion()
    except Exception as error:
        failed = True
    if failed and not terminal and delivered == ("" if provider == "palm" else "hello"):
        interrupted += 1
        print(f"LEGACY_INTERRUPTION_OK: {provider}, {'before_commit' if provider == 'palm' else 'after_delivery'}")
    else:
        failures.append(f"{label}: failed={failed}, terminal={terminal}, delivered={delivered!r}")
oai.close()
claude.close()
gemini.close()
assert not failures, "\n".join(failures)
assert (successes, rejected, interrupted) == (12, 36, 6), (successes, rejected, interrupted)
print("LEGACY_SDK_OK: 12 Chat paths, 36 pre-send rejections, 5 delivered interruptions and 1 buffered failure (Python)")
