import io
import os
import wave

from openai import APIError, OpenAI


client = OpenAI(base_url=os.environ["GATEWAY_SDK_URL"] + "/v1", api_key=os.environ["GATEWAY_SDK_KEY"], max_retries=0)
model = "gpt-4o-mini"
for encoding in ("float", "base64"):
    vectors = client.embeddings.create(model=model, input=["hello", "world"], encoding_format=encoding)
    assert len(vectors.data) == 2 and vectors.data[0].index == 0

image = client.images.generate(model=model, prompt="house", n=1)
assert image.data[0].b64_json == "AAECAw=="
events = list(client.images.generate(model=model, prompt="house", n=1, stream=True))
assert [event.type for event in events] == ["image_generation.partial_image", "image_generation.completed"]
assert events[-1].b64_json == "AAECAw==" and events[-1].usage.total_tokens == 30

speech = client.audio.speech.create(model=model, input="hello", voice="alloy", response_format="pcm")
assert speech.read() == bytes([0, 1, 2, 3])
audio = io.BytesIO()
with wave.open(audio, "wb") as wav:
    wav.setnchannels(1)
    wav.setsampwidth(2)
    wav.setframerate(24000)
    wav.writeframes(bytes(480))
for stream in (False, True):
    transcript = client.audio.transcriptions.create(model=model, file=("input.wav", audio.getvalue(), "audio/wav"), stream=stream)
    if stream:
        events = list(transcript)
        assert [event.type for event in events] == ["transcript.text.delta", "transcript.text.done"]
        assert events[-1].text == "hello"
    else:
        assert transcript.text == "hello"

events = []
try:
    for event in client.images.generate(model=model, prompt="interrupt", n=1, stream=True):
        events.append(event)
except APIError:
    assert len(events) == 1 and events[0].type == "image_generation.partial_image"
else:
    raise AssertionError("interrupted image stream unexpectedly completed")
print("MEDIA_SDK_OK: 7 normal paths and image interruption (Python)")
