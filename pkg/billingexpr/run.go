package billingexpr

import (
	"fmt"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/shopspring/decimal"
	"math"
	"strings"
	"time"
)

func RunExpr(s string, p TokenParams) (decimal.Decimal, TraceResult, error) {
	return RunExprWithRequest(s, p, RequestInput{})
}
func RunExprWithRequest(s string, p TokenParams, r RequestInput) (decimal.Decimal, TraceResult, error) {
	return RunExprByHashWithRequest(s, ExprHashString(s), p, r)
}
func RunExprByHash(s, h string, p TokenParams) (decimal.Decimal, TraceResult, error) {
	return RunExprByHashWithRequest(s, h, p, RequestInput{})
}
func RunExprByHashWithRequest(s, h string, p TokenParams, r RequestInput) (decimal.Decimal, TraceResult, error) {
	plan, e := compilePlan(s, h, r.UsageSchema)
	if e != nil {
		return decimal.Zero, TraceResult{}, e
	}
	state := evaluation{request: r, trace: TraceResult{RequestRules: append([]RequestRuleTrace(nil), plan.rules...)}, usage: map[string]any{}}
	if e = ValidateAttributes(r.Attributes); e != nil {
		return decimal.Zero, state.trace, e
	}
	state.tokens = map[string]int64{"p": p.P, "c": p.C, "len": p.Len, "cr": p.CR, "cc": p.CC, "cc1h": p.CC1h, "img": p.Img, "img_cr": p.ImgCR, "img_o": p.ImgO, "ai": p.AI, "ai_cr": p.AICR, "ao": p.AO, "reasoning": p.Reasoning}
	for _, value := range state.tokens {
		if value < 0 || value > math.MaxInt32 {
			return decimal.Zero, state.trace, fmt.Errorf("invalid billing token quantity")
		}
	}
	if plan.usedVars["image_count"] {
		if r.ImageCount == nil || *r.ImageCount < 1 || *r.ImageCount > dto.MaxImageN {
			return decimal.Zero, state.trace, fmt.Errorf("image_count requires an integer between 1 and %d", dto.MaxImageN)
		}
		count := *r.ImageCount
		state.tokens["image_count"] = int64(count)
		state.trace.ImageCount = &count
	}
	for key := range plan.usedUsage {
		raw, ok := r.Usage[key]
		if !ok || raw == nil {
			return decimal.Zero, state.trace, fmt.Errorf("billing usage %q is missing", key)
		}
		value, e := NormalizeUsageValue(r.UsageSchema.Fields[key], raw)
		if e != nil {
			return decimal.Zero, state.trace, e
		}
		if r.UsageSchema.Fields[key].Type == "number" {
			value, e = decimal.NewFromString(value.(string))
			if e != nil {
				return decimal.Zero, state.trace, e
			}
		}
		state.usage[key] = value
	}
	for zone := range plan.zones {
		calendar, ok := r.Calendars[zone]
		if !ok || r.FrozenAt.IsZero() {
			return decimal.Zero, state.trace, fmt.Errorf("billing calendar is not frozen")
		}
		if e := validateCalendar(calendar, r.FrozenAt); e != nil {
			return decimal.Zero, state.trace, e
		}
	}
	value, e := state.eval(plan.root)
	if e != nil {
		return decimal.Zero, state.trace, e
	}
	number, ok := value.(decimal.Decimal)
	if !ok || number.IsNegative() {
		return decimal.Zero, state.trace, fmt.Errorf("invalid billing expression result")
	}
	return number, state.trace, nil
}

// FreezeCalendars resolves timezone rules only while creating the quote. The
// offset and calendar fields become part of its immutable hash.
func FreezeCalendars(s string, schema *UsageSchema, at time.Time) (map[string]FrozenCalendar, error) {
	plan, e := CompileWithUsageSchema(s, schema)
	if e != nil {
		return nil, e
	}
	if len(plan.zones) == 0 {
		return nil, nil
	}
	if at.IsZero() {
		return nil, fmt.Errorf("billing time is not frozen")
	}
	values := make(map[string]FrozenCalendar, len(plan.zones))
	for zone := range plan.zones {
		loc, e := time.LoadLocation(zone)
		if e != nil {
			return nil, e
		}
		t := at.In(loc)
		_, offset := t.Zone()
		values[zone] = FrozenCalendar{Year: t.Year(), Month: int(t.Month()), Day: t.Day(), Weekday: int(t.Weekday()), Hour: t.Hour(), Minute: t.Minute(), UTCOffsetSeconds: offset}
	}
	return values, nil
}
func validateCalendar(c FrozenCalendar, at time.Time) error {
	if c.UTCOffsetSeconds < -86400 || c.UTCOffsetSeconds > 86400 {
		return fmt.Errorf("invalid frozen billing calendar")
	}
	t := at.UTC().Add(time.Duration(c.UTCOffsetSeconds) * time.Second)
	if c.Year != t.Year() || c.Month != int(t.Month()) || c.Day != t.Day() || c.Weekday != int(t.Weekday()) || c.Hour != t.Hour() || c.Minute != t.Minute() {
		return fmt.Errorf("invalid frozen billing calendar")
	}
	return nil
}
func checkDecimal(d decimal.Decimal) error {
	// Bound work before an intermediate can allocate an unbounded power of ten.
	if d.Exponent() < -512 || d.Exponent() > 512 || len(d.Coefficient().String()) > 512 {
		return fmt.Errorf("billing decimal exceeds execution range")
	}
	return nil
}

type evaluation struct {
	request RequestInput
	trace   TraceResult
	tokens  map[string]int64
	usage   map[string]any
}

func (e *evaluation) eval(n *operation) (value any, err error) {
	defer func() {
		if d, ok := value.(decimal.Decimal); ok && err == nil {
			err = checkDecimal(d)
		}
	}()
	switch n.kind {
	case "number":
		return n.number, nil
	case "string":
		return n.text, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "nil":
		return nil, nil
	case "variable":
		return decimal.NewFromInt(e.tokens[n.text]), nil
	case "conditional":
		condition, err := e.eval(n.args[0])
		if err != nil {
			return nil, err
		}
		flag, ok := condition.(bool)
		if !ok {
			return nil, fmt.Errorf("billing condition must be boolean")
		}
		if flag {
			if n.rule >= 0 {
				e.trace.RequestRules[n.rule].Matched = true
			}
			return e.eval(n.args[1])
		}
		return e.eval(n.args[2])
	case "unary":
		value, err := e.eval(n.args[0])
		if err != nil {
			return nil, err
		}
		if n.text == "!" || n.text == "not" {
			flag, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("billing logical operand must be boolean")
			}
			return !flag, nil
		}
		d, ok := value.(decimal.Decimal)
		if !ok {
			return nil, fmt.Errorf("billing operand must be numeric")
		}
		if n.text == "-" {
			return d.Neg(), nil
		}
		return d, nil
	case "binary":
		return e.binary(n)
	case "call":
		return e.call(n)
	}
	return nil, fmt.Errorf("unsupported billing operation")
}
func (e *evaluation) binary(n *operation) (any, error) {
	left, err := e.eval(n.args[0])
	if err != nil {
		return nil, err
	}
	if n.text == "&&" || n.text == "and" || n.text == "||" || n.text == "or" {
		flag, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("billing logical operand must be boolean")
		}
		and := n.text == "&&" || n.text == "and"
		if and && !flag || !and && flag {
			return flag, nil
		}
		right, err := e.eval(n.args[1])
		if err != nil {
			return nil, err
		}
		if _, ok := right.(bool); !ok {
			return nil, fmt.Errorf("billing logical operand must be boolean")
		}
		return right, nil
	}
	right, err := e.eval(n.args[1])
	if err != nil {
		return nil, err
	}
	if n.text == "==" || n.text == "!=" {
		equal := false
		switch a := left.(type) {
		case nil:
			equal = right == nil
		case decimal.Decimal:
			b, ok := right.(decimal.Decimal)
			equal = ok && a.Equal(b)
		case string:
			b, ok := right.(string)
			equal = ok && a == b
		case bool:
			b, ok := right.(bool)
			equal = ok && a == b
		}
		if n.text == "!=" {
			equal = !equal
		}
		return equal, nil
	}
	a, ok := left.(decimal.Decimal)
	b, ok2 := right.(decimal.Decimal)
	if !ok || !ok2 {
		return nil, fmt.Errorf("billing operands must be numeric")
	}
	switch n.text {
	case "+":
		return a.Add(b), nil
	case "-":
		return a.Sub(b), nil
	case "*":
		return a.Mul(b), nil
	case "/":
		if b.IsZero() {
			return nil, fmt.Errorf("billing division by zero")
		}
		return a.DivRound(b, 36), nil
	case "<":
		return a.LessThan(b), nil
	case "<=":
		return a.LessThanOrEqual(b), nil
	case ">":
		return a.GreaterThan(b), nil
	case ">=":
		return a.GreaterThanOrEqual(b), nil
	}
	return nil, fmt.Errorf("unsupported billing operator")
}
func (e *evaluation) call(n *operation) (any, error) {
	args := make([]any, len(n.args))
	for i, node := range n.args {
		value, err := e.eval(node)
		if err != nil {
			return nil, err
		}
		args[i] = value
	}
	switch n.text {
	case "u":
		return e.usage[n.args[0].text], nil
	case "attr", "attr_num":
		value := e.request.Attributes[n.args[0].text]
		if n.text == "attr_num" {
			return AttributeNumber(value)
		}
		return value, nil
	case "hour", "minute", "weekday", "month", "day":
		c := e.request.Calendars[n.args[0].text]
		values := map[string]int{"hour": c.Hour, "minute": c.Minute, "weekday": c.Weekday, "month": c.Month, "day": c.Day}
		return decimal.NewFromInt(int64(values[n.text])), nil
	case "tier":
		d, ok := args[1].(decimal.Decimal)
		if !ok {
			return nil, fmt.Errorf("billing tier must be numeric")
		}
		e.trace.MatchedTier = n.args[0].text
		return d, nil
	case "has":
		text, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("billing substring must be text")
		}
		if args[0] == nil || text == "" {
			return false, nil
		}
		return strings.Contains(fmt.Sprint(args[0]), text), nil
	}
	d, ok := args[0].(decimal.Decimal)
	if !ok {
		return nil, fmt.Errorf("billing function requires numeric operands")
	}
	switch n.text {
	case "fixed":
		return d.Shift(6), nil
	case "abs":
		return d.Abs(), nil
	case "ceil":
		return d.Ceil(), nil
	case "floor":
		return d.Floor(), nil
	case "max", "min":
		b, ok := args[1].(decimal.Decimal)
		if !ok {
			return nil, fmt.Errorf("billing function requires numeric operands")
		}
		if n.text == "max" && b.GreaterThan(d) || n.text == "min" && b.LessThan(d) {
			return b, nil
		}
		return d, nil
	}
	return nil, fmt.Errorf("unsupported billing function")
}
