package service

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strconv"
	"testing"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// The routing error code table repeats the names of the native classifier,
// which is upstream text. Every class must agree with the native verdict for
// an upstream error carrying that name as its code or as its type: account
// and server names fail the channel even with 400, which the classifier
// otherwise ignores, and request names are ignored even with 502, which it
// otherwise fails. The classifier fails account and server names alike; the
// local split is stated here: the five names below are the upstream's own
// failure, every other failing name is an account signal, and only account
// signals keep an upstream error with context overflow wording a failure.
func TestRouteErrorCodesAgreeWithNativeClassifier(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "route-model", StartTime: time.Now()}
	overflow := "This model's maximum context length is 128000 tokens."
	server := map[string]bool{"overloaded_error": true, "server_error": true, "internal_error": true, "service_unavailable": true, "model_not_found": true}
	for code, class := range nativeRouteErrorCodes {
		require.Equal(t, server[code], class == routeErrorServer, code)
		status, want := http.StatusBadRequest, perfmetrics.OutcomeFailure
		if class == routeErrorRequest {
			status, want = http.StatusBadGateway, perfmetrics.OutcomeIgnored
		}
		for _, entry := range []string{`"code":%q`, `"type":%q`} {
			field := fmt.Sprintf(entry, code)
			apiErr := upstreamError(t, status, `{"error":{"message":"m",`+field+`}}`, false)
			require.Equal(t, want, perfmetrics.ClassifyRelayOutcome(context.Background(), info, apiErr), field)

			apiErr = upstreamError(t, http.StatusInternalServerError, `{"error":{"message":"`+overflow+`",`+field+`}}`, false)
			require.Equal(t, class != routeErrorAccount, (&routeAttempt{}).requestContextOverflow(apiErr), field)
		}
		require.Equal(t, code, routeErrorCode(code))
	}
	require.Equal(t, string(types.ErrorCodeDoRequestFailed), routeErrorCode(string(types.ErrorCodeDoRequestFailed)))
	require.Equal(t, routeUnrecognizedErrorCode, routeErrorCode(errors.New("x").Error()))
}

// The table lists every name the upstream branch of the native classifier
// names, read from its source, so an upstream sync that adds or moves a name
// fails here until the table is reviewed.
func TestRouteErrorCodesListEveryNativeName(t *testing.T) {
	native := nativeUpstreamVerdicts(t)
	require.NotEmpty(t, native)
	listed := map[string]perfmetrics.Outcome{}
	for code, class := range nativeRouteErrorCodes {
		listed[code] = perfmetrics.OutcomeFailure
		if class == routeErrorRequest {
			listed[code] = perfmetrics.OutcomeIgnored
		}
	}
	require.Equal(t, native, listed)
}

// nativeUpstreamVerdicts returns the names in the switch inside the loop over
// code and type of classifyFailure, with the verdict of each case.
func nativeUpstreamVerdicts(t *testing.T) map[string]perfmetrics.Outcome {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../pkg/perf_metrics/outcome.go", nil, 0)
	require.NoError(t, err)
	verdicts := map[string]perfmetrics.Outcome{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "classifyFailure" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			loop, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			ast.Inspect(loop.Body, func(node ast.Node) bool {
				clause, ok := node.(*ast.CaseClause)
				if !ok || len(clause.List) == 0 {
					return true
				}
				verdict := clauseVerdict(t, clause)
				for _, expr := range clause.List {
					literal, ok := expr.(*ast.BasicLit)
					require.True(t, ok && literal.Kind == token.STRING, "a case of the native switch is not a string literal")
					name, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					verdicts[name] = verdict
				}
				return false
			})
			return false
		})
	}
	return verdicts
}

func clauseVerdict(t *testing.T, clause *ast.CaseClause) perfmetrics.Outcome {
	t.Helper()
	require.NotEmpty(t, clause.Body)
	ret, ok := clause.Body[len(clause.Body)-1].(*ast.ReturnStmt)
	require.True(t, ok && len(ret.Results) == 1, "a case of the native switch does not end in a return")
	ident, ok := ret.Results[0].(*ast.Ident)
	require.True(t, ok, "a case of the native switch returns no outcome name")
	switch ident.Name {
	case "OutcomeFailure":
		return perfmetrics.OutcomeFailure
	case "OutcomeIgnored":
		return perfmetrics.OutcomeIgnored
	}
	t.Fatalf("unknown native outcome %s", ident.Name)
	return ""
}
