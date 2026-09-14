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
for provider in ("nova", "coze", "xunfei"):
    model = "special-" + provider
    for protocol in ("chat", "responses", "anthropic", "gemini"):
        for mode in ("json", "stream"):
            label = f"special-{provider}-{protocol}-{mode}"
            supported = protocol == "chat" and (provider != "nova" or mode == "json")
            try:
                if protocol == "chat":
                    request = {"model": model, "messages": [{"role": "user", "content": label}]}
                    if mode == "stream":
                        deltas = roles = 0
                        with oai.chat.completions.stream(**request) as stream:
                            for event in stream:
                                if event.type == "chunk":
                                    for choice in event.chunk.choices:
                                        assert choice.index == 0, choice
                                        roles += choice.delta.role is not None
                                        deltas += bool(choice.delta.content)
                            response = stream.get_final_completion()
                        assert roles == 1 and deltas >= 2, (roles, deltas)
                    else:
                        response = oai.chat.completions.create(**request)
                    assert supported, "unsupported path accepted"
                    assert len(response.choices) == 1, response.choices
                    message = response.choices[0].message
                    assert message.role == "assistant", message.role
                    assert message.content == "hello", message.content
                    assert response.choices[0].finish_reason == "stop", response.choices[0].finish_reason
                elif protocol == "responses":
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
                assert supported, "unsupported path accepted"
                successes += 1
                print(f"SPECIAL_PATH_OK: {label}")
            except Exception as error:
                status = getattr(error, "status_code", None) or getattr(error, "code", None)
                expected_status = 400 if provider == "nova" and protocol == "chat" and mode == "stream" else 503
                if not supported and status == expected_status:
                    rejected += 1
                    print(f"SPECIAL_PATH_REJECTED: {label}")
                else:
                    failures.append(f"{label}: {type(error).__name__}: {error}")
    if provider != "nova":
        label = f"special-{provider}-chat-interrupt"
        delivered, terminal, failed = "", False, False
        try:
            with oai.chat.completions.stream(model=model, messages=[{"role": "user", "content": label}]) as stream:
                for event in stream:
                    if event.type == "content.delta":
                        delivered += event.delta
                    if event.type == "chunk":
                        terminal = terminal or any(choice.finish_reason for choice in event.chunk.choices)
                stream.get_final_completion()
        except Exception:
            failed = True
        if failed and not terminal and delivered == "hello":
            interrupted += 1
            print(f"SPECIAL_INTERRUPTION_OK: {provider}")
        else:
            failures.append(f"{label}: failed={failed}, terminal={terminal}, delivered={delivered!r}")
try:
    oai.chat.completions.create(model="special-coze-poll-failure", messages=[{"role": "user", "content": "special-coze-chat-poll_failure"}])
    failures.append("Coze status query failure was accepted")
except Exception as error:
    if getattr(error, "status_code", None) != 502:
        failures.append(f"Coze status query: {type(error).__name__}: {error}")
    else:
        print("SPECIAL_POLL_FAILURE_OK: accepted Coze chat was not replayed")
oai.close()
claude.close()
gemini.close()
assert not failures, "\n".join(failures)
assert (successes, rejected, interrupted) == (5, 19, 2), (successes, rejected, interrupted)
print("SPECIAL_SDK_OK: 5 Chat paths, 19 pre-send rejections, 2 delivered interruptions, 1 accepted poll failure (Python)")
