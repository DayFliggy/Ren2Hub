#!/usr/bin/env python3
"""官方 Python SDK 单次回环检查。凭据仅从 stdin 读取，错误不输出响应正文。"""
import json
import hashlib
from importlib import metadata
import platform
import sys
from urllib.parse import urlsplit

import httpx
import openai

CASES = ('chat_json', 'chat_sse', 'responses_json', 'responses_sse')
TEXT = 'local answer'
USAGE = {'input_tokens': 10, 'output_tokens': 2, 'total_tokens': 12}


class ContractError(RuntimeError):
    pass


def require(condition, code):
    if not condition:
        raise ContractError(code)


def usage(value, chat=False):
    require(value is not None, 'sdk_usage_missing')
    actual = {'input_tokens': value.prompt_tokens if chat else value.input_tokens,
              'output_tokens': value.completion_tokens if chat else value.output_tokens,
              'total_tokens': value.total_tokens}
    require(actual == USAGE, 'sdk_usage_mismatch')


class LoopbackTransport(httpx.BaseTransport):
    def __init__(self, origin):
        self.origin, self.requests = origin, 0
        self.transport = httpx.HTTPTransport(retries=0, trust_env=False)

    def handle_request(self, request):
        target = urlsplit(str(request.url))
        require((target.scheme, target.netloc) == self.origin
                and target.path in ('/v1/chat/completions', '/v1/responses')
                and not target.query and not target.fragment, 'sdk_non_loopback_request_rejected')
        self.requests += 1
        return self.transport.handle_request(request)

    def close(self):
        self.transport.close()


def run(config):
    require(isinstance(config, dict) and config.get('case') in CASES, 'sdk_input_invalid')
    target = urlsplit(config.get('base_url', ''))
    require(target.scheme == 'http' and target.hostname == '127.0.0.1' and target.port is not None
            and target.username is None and target.path == '/v1' and not target.query and not target.fragment,
            'sdk_explicit_loopback_required')
    require(isinstance(config.get('api_key'), str) and config['api_key']
            and isinstance(config.get('model'), str) and config['model'], 'sdk_credentials_or_model_missing')
    transport = LoopbackTransport((target.scheme, target.netloc))
    case, model = config['case'], config['model']
    with httpx.Client(transport=transport, trust_env=False, follow_redirects=False, timeout=20) as http:
        with openai.OpenAI(api_key=config['api_key'], base_url=config['base_url'],
                           max_retries=0, timeout=20, http_client=http) as client:
            if case.startswith('chat_'):
                options = {'model': model, 'messages': [{'role': 'user', 'content': 'local acceptance'}], 'max_tokens': 8}
                if case == 'chat_json':
                    result = client.chat.completions.create(**options)
                    require(result.model == model and len(result.choices) == 1, 'sdk_chat_identity_mismatch')
                    choice = result.choices[0]
                    require(choice.message.content == TEXT and choice.message.role == 'assistant'
                            and choice.finish_reason == 'stop', 'sdk_chat_result_mismatch')
                    usage(result.usage, chat=True)
                else:
                    text, finishes, usages = '', [], 0
                    with client.chat.completions.create(**options, stream=True, stream_options={'include_usage': True}) as stream:
                        for chunk in stream:
                            require(chunk.model == model, 'sdk_chat_stream_identity_mismatch')
                            for choice in chunk.choices:
                                require(choice.index == 0, 'sdk_chat_choice_mismatch')
                                if choice.delta.content:
                                    require(not finishes, 'sdk_chat_content_after_terminal')
                                    text += choice.delta.content
                                if choice.finish_reason is not None:
                                    finishes.append(choice.finish_reason)
                            if chunk.usage is not None:
                                usage(chunk.usage, chat=True)
                                usages += 1
                    require(text == TEXT and finishes == ['stop'] and usages > 0, 'sdk_chat_stream_terminal_or_usage_missing')
                terminal = 'stop'
            else:
                options = {'model': model, 'input': 'local acceptance', 'max_output_tokens': 32}
                if case == 'responses_json':
                    response = client.responses.create(**options)
                else:
                    response, text, events = None, '', []
                    with client.responses.create(**options, stream=True) as stream:
                        for event in stream:
                            require(response is None, 'sdk_responses_event_after_terminal')
                            events.append(event.type)
                            require(event.type not in ('response.failed', 'response.incomplete', 'error'), 'sdk_responses_failed')
                            if event.type == 'response.output_text.delta':
                                text += event.delta
                            if event.type == 'response.completed':
                                response = event.response
                    require(events and events[0] == 'response.created' and events[-1] == 'response.completed'
                            and text == TEXT, 'sdk_responses_stream_terminal_or_text_missing')
                require(response is not None and response.model == model and response.status == 'completed'
                        and response.output_text == TEXT and response.error is None, 'sdk_responses_result_mismatch')
                usage(response.usage)
                terminal = 'completed'
    require(transport.requests == 1, 'sdk_request_replayed')
    return {'success': True, 'sdk': 'python', 'sdk_version': openai.__version__, 'runtime_version': platform.python_version(),
            'package_metadata_sha256': hashlib.sha256(metadata.distribution('openai').read_text('METADATA').encode()).hexdigest(),
            'case': case, 'requests': transport.requests, 'max_retries': 0, 'text': TEXT, 'usage': USAGE, 'terminal': terminal}


def main():
    try:
        raw = sys.stdin.buffer.read(16385)
        require(len(raw) <= 16384, 'sdk_input_too_large')
        result = run(json.loads(raw))
    except Exception as error:
        result = {'success': False, 'error_code': str(error) if isinstance(error, ContractError) else 'sdk_call_failed',
                  'error_type': type(error).__name__}
    print(json.dumps(result))
    return 0 if result['success'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
