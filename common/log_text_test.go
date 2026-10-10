package common

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Server log lines are English. DEFAULT_LANGUAGE=zh-CN prints the Chinese text
// of the lines that have one in log_text.zh-CN.json; other lines stay English.
func TestServerLogLanguage(t *testing.T) {
	t.Cleanup(func() { SetLogLanguage("") })
	const line = "channel #%d has %d unfinished tasks"
	assert.Equal(t, "channel #7 has 2 unfinished tasks", LogText(line, 7, 2))
	SetLogLanguage("zh-CN")
	assert.Equal(t, "渠道 #7 未完成的任务有: 2", LogText(line, 7, 2))
	assert.Equal(t, "a line without a Chinese text: x", LogText("a line without a Chinese text: %s", "x"))
	assert.Equal(t, "任务进度轮询开始", LogText("task progress polling started"))
	SetLogLanguage("en")
	assert.Equal(t, "channel #7 has 2 unfinished tasks", LogText(line, 7, 2))
}

// Each Chinese line must give every argument of its English key the same verb,
// or LogText would print %!(EXTRA ...) or %!d(MISSING) in place of the
// arguments. A Chinese line may reorder arguments with indexed verbs (%[2]d)
// and quote a %q string with 「%s」 instead.
func TestServerLogTextsKeepTheirVerbs(t *testing.T) {
	var texts map[string]string
	require.NoError(t, Unmarshal(logTextZhCN, &texts))
	require.NotEmpty(t, texts)
	for english, chinese := range texts {
		assert.Equal(t, logArgumentVerbs(english), logArgumentVerbs(chinese), english)
	}
}

// logArgumentVerbs maps each argument index of a format to its verb.
func logArgumentVerbs(format string) map[int]byte {
	verbs := map[int]byte{}
	next := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.", format[i]) >= 0 {
			i++
		}
		if i < len(format) && format[i] == '[' {
			end := strings.IndexByte(format[i:], ']')
			if end < 0 {
				break
			}
			index, err := strconv.Atoi(format[i+1 : i+end])
			if err != nil {
				break
			}
			next = index - 1
			i += end + 1
		}
		if i >= len(format) || format[i] == '%' {
			continue
		}
		verb := format[i]
		if verb == 'q' {
			verb = 's'
		}
		verbs[next] = verb
		next++
	}
	return verbs
}
