package billingexpr

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timeReplayFixture struct {
	Expression string
	Headers    map[string]string
	Calls      []EnvironmentCall
}

func TestMonotonicTimeSequenceReplaysInAnotherProcess(t *testing.T) {
	first := time.Now()
	state, err := captureTimeState(first)
	require.NoError(t, err)
	require.NotZero(t, state.Wall&(1<<63))
	state.Wall -= 5 << 30
	state.Ext += int64(3 * time.Second)
	second, err := restoreTimeState(state, first.Location())
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, second.Sub(first))
	assert.Equal(t, -5*time.Second, second.Round(0).Sub(first.Round(0)))
	const expression = `let a=now(); let b=now(); b.Sub(a).Seconds() + (b.After(a) ? 10 : 0) + (b.Equal(a.Add(duration("3s"))) ? 100 : 0) + (b.String() == header("expected") ? 1000 : 0)`
	evaluation := NewEvaluation()
	sequence := []time.Time{first, second}
	evaluation.Now = func() time.Time { at := sequence[0]; sequence = sequence[1:]; return at }
	headers := map[string]string{"expected": second.String()}
	value, _, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Headers: headers, Evaluation: evaluation})
	require.NoError(t, err)
	assert.Equal(t, float64(1113), value)
	encoded, err := common.Marshal(timeReplayFixture{Expression: expression, Headers: headers, Calls: evaluation.Calls})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "time-evidence.json")
	require.NoError(t, os.WriteFile(path, encoded, 0600))
	executable, err := os.Executable()
	require.NoError(t, err)
	process := exec.Command(executable, "-test.run=^TestTimeReplayChildProcess$", "-test.timeout=15s")
	process.Env = append(os.Environ(), "REN2HUB_TEST_TIME_EVIDENCE="+path)
	output, err := process.CombinedOutput()
	require.NoError(t, err, string(output))
}

func TestTimeReplayChildProcess(t *testing.T) {
	path := os.Getenv("REN2HUB_TEST_TIME_EVIDENCE")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var fixture timeReplayFixture
	require.NoError(t, common.Unmarshal(data, &fixture))
	value, _, err := RunExprWithRequest(fixture.Expression, TokenParams{}, RequestInput{Headers: fixture.Headers, Evaluation: ReplayEvaluation(fixture.Calls)})
	require.NoError(t, err)
	assert.Equal(t, float64(1113), value)
}

func TestTimeSnapshotRejectsChangedLayoutAndToolchain(t *testing.T) {
	require.NoError(t, validateTimeLayout(reflect.TypeOf(time.Time{})))
	for _, changed := range []any{
		struct {
			wall int64
			ext  int64
			loc  *time.Location
		}{},
		struct {
			wall uint64
			loc  *time.Location
			ext  int64
		}{},
		struct {
			wall     uint64
			ext      int64
			location *time.Location
		}{},
		struct {
			wall  uint64
			ext   int64
			loc   *time.Location
			extra byte
		}{},
	} {
		require.Error(t, validateTimeLayout(reflect.TypeOf(changed)))
	}
	state, err := captureTimeState(time.Now())
	require.NoError(t, err)
	state.Toolchain = "unvalidated-future-toolchain"
	_, err = restoreTimeState(state, time.UTC)
	require.Error(t, err)
}

func TestLocalAndDynamicTimeMembersReplayTheirOriginalEnvironment(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	time.Local = time.FixedZone("billing-before", 2*3600)
	at := time.Date(2026, 10, 1, 15, 0, 0, 0, time.Local)
	const expression = `let a=now(); let method=a[param("method")]; let b=method(); let z=b.Location(); let later=now(z); (b.GoString() == header("expected") ? 1 : 0) + (later.Location()==z ? 10 : 0) + (b == later ? 100 : 0) + b.Hour()`
	evaluation := NewEvaluation()
	evaluation.Now = func() time.Time { return at }
	input := RequestInput{Body: []byte(`{"method":"Local"}`), Headers: map[string]string{"expected": at.GoString()}, Evaluation: evaluation}
	value, _, err := RunExprWithRequest(expression, TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, float64(126), value)
	data, err := common.Marshal(evaluation.Calls)
	require.NoError(t, err)
	var calls []EnvironmentCall
	require.NoError(t, common.Unmarshal(data, &calls))
	time.Local = time.FixedZone("billing-after", -9*3600)
	input.Evaluation = ReplayEvaluation(calls)
	replayed, _, err := RunExprWithRequest(expression, TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, value, replayed)
	for _, expression := range []string{
		`param("missing")?.Local() == nil ? p : 0`,
		`param("mapping")[param("key")] * p`,
	} {
		value, _, err := RunExprWithRequest(expression, TokenParams{P: 3}, RequestInput{Body: []byte(`{"mapping":{"rate":1},"key":"rate"}`), Evaluation: NewEvaluation()})
		require.NoError(t, err)
		assert.Equal(t, float64(3), value)
	}
}
