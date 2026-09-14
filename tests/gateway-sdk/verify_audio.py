import io
import os
import wave

from httpx import RemoteProtocolError
from openai import APIConnectionError, APIError, OpenAI


client = OpenAI(base_url=os.environ["GATEWAY_SDK_URL"] + "/v1", api_key=os.environ["GATEWAY_SDK_KEY"], max_retries=0)
for model in ("speech-minimax", "speech-volc-http", "speech-volc-ws"):
    speech = client.audio.speech.create(model=model, input="hello", voice="alloy", response_format="pcm")
    assert speech.read() == bytes([0, 1, 2, 3])

audio = io.BytesIO()
with wave.open(audio, "wb") as wav:
    wav.setnchannels(1)
    wav.setsampwidth(2)
    wav.setframerate(24000)
    wav.writeframes(bytes(480))
for response_format in ("json", "text"):
    transcript = client.audio.transcriptions.create(model="whisper-cloudflare", file=("input.wav", audio.getvalue(), "audio/wav"), response_format=response_format)
    assert (transcript.text if response_format == "json" else transcript) == "hello"

try:
    client.audio.transcriptions.create(model="whisper-cloudflare", file=("input.wav", audio.getvalue(), "audio/wav"), stream=True)
except APIError as error:
    assert error.status_code == 400
else:
    raise AssertionError("unsupported Cloudflare stream accepted")

try:
    client.audio.speech.create(model="whisper-cloudflare", input="hello", voice="alloy")
except APIError as error:
    assert error.status_code == 503
else:
    raise AssertionError("unsupported Cloudflare speech accepted")

try:
    client.audio.speech.create(model="speech-volc-ws", input="interrupt", voice="alloy", response_format="pcm").read()
except (APIConnectionError, RemoteProtocolError):
    # The official binary response reader reports an interrupted HTTP body.
    pass
else:
    raise AssertionError("truncated binary speech unexpectedly completed")
print("AUDIO_SDK_OK: 5 normal paths, 2 pre-send rejections, 1 binary interruption (Python)")
