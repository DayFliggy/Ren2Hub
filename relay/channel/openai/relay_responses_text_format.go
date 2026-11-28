package openai

import "github.com/MoeForge/moego/ojson"

// fillResponsesTextFormat restores the default text.format some upstreams drop when tools are present.
// OpenAI always returns text.format next to text, and strict clients fail to decode a text object without it.
func fillResponsesTextFormat(payload []byte, textPath string) []byte {
	text := ojson.GetBytes(payload, textPath)
	if !text.IsObject() || text.Get("format").Exists() {
		return payload
	}
	patched, err := ojson.SetRawBytes(payload, textPath+".format", []byte(`{"type":"text"}`))
	if err != nil {
		return payload
	}
	return patched
}
