package billingexpr

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/builtin"
)

// Evaluation records environment reads, never expression results. Each billing
// phase owns a separate instance; compiled programs can be shared concurrently.
type Evaluation struct {
	Calls               []EnvironmentCall `json:"calls"`
	replay              bool
	next                int
	Now                 func() time.Time `json:"-"`
	locationIDs         map[*time.Location]int
	locations           map[int]*time.Location
	locationDefinitions map[int]recordedLocation
	nextLocationID      int
}

type EnvironmentCall struct {
	Name      string          `json:"name"`
	Arguments []RecordedValue `json:"arguments"`
	Result    RecordedValue   `json:"result"`
}

// RecordedValue preserves Go types used by Expr. JSON numbers alone would turn
// integer operands into floats and change %, comparisons, and map operations.
type RecordedValue struct {
	Kind      string                   `json:"kind"`
	Text      string                   `json:"text,omitempty"`
	Offset    int                      `json:"offset,omitempty"`
	TimeState *timeState               `json:"time_state,omitempty"`
	Location  *recordedLocation        `json:"location,omitempty"`
	Items     []RecordedValue          `json:"items,omitempty"`
	Fields    map[string]RecordedValue `json:"fields,omitempty"`
}

type recordedLocation struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	UTC  bool   `json:"utc,omitempty"`
	Data []byte `json:"data,omitempty"`
}

func NewEvaluation() *Evaluation { return &Evaluation{Calls: []EnvironmentCall{}} }
func ReplayEvaluation(calls []EnvironmentCall) *Evaluation {
	return &Evaluation{Calls: calls, replay: true}
}

func (e *Evaluation) Complete() error {
	if e != nil && e.replay && e.next != len(e.Calls) {
		return fmt.Errorf("billing evaluation evidence has unused calls")
	}
	return nil
}

func (e *Evaluation) invoke(name string, args []any, run func() (any, error)) (any, error) {
	if e == nil {
		return run()
	}
	encoded := make([]RecordedValue, len(args))
	for i, arg := range args {
		var err error
		encoded[i], err = e.recordTypedValue(arg, false)
		if err != nil {
			return nil, fmt.Errorf("billing environment argument cannot be recorded")
		}
	}
	if e.replay {
		if e.next >= len(e.Calls) {
			return nil, fmt.Errorf("billing evaluation evidence is incomplete")
		}
		call := e.Calls[e.next]
		if call.Name != name || !reflect.DeepEqual(call.Arguments, encoded) {
			return nil, fmt.Errorf("billing evaluation evidence does not match")
		}
		e.next++
		return e.restoreValue(call.Result)
	}
	value, err := run()
	if err != nil {
		return nil, err
	}
	result, err := e.recordTypedValue(value, true)
	if err != nil {
		return nil, fmt.Errorf("billing environment result cannot be recorded")
	}
	e.Calls = append(e.Calls, EnvironmentCall{Name: name, Arguments: encoded, Result: result})
	return value, nil
}

func (e *Evaluation) recordTypedValue(value any, freezeZone bool) (RecordedValue, error) {
	switch v := value.(type) {
	case nil:
		return RecordedValue{Kind: "nil"}, nil
	case bool:
		return RecordedValue{Kind: "bool", Text: strconv.FormatBool(v)}, nil
	case string:
		return RecordedValue{Kind: "string", Text: v}, nil
	case int:
		return RecordedValue{Kind: "int", Text: strconv.Itoa(v)}, nil
	case int64:
		return RecordedValue{Kind: "int64", Text: strconv.FormatInt(v, 10)}, nil
	case float64:
		return RecordedValue{Kind: "float64", Text: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case time.Duration:
		return RecordedValue{Kind: "duration", Text: strconv.FormatInt(int64(v), 10)}, nil
	case time.Time:
		_, offset := v.Zone()
		state, err := captureTimeState(v)
		if err != nil {
			return RecordedValue{}, err
		}
		location, err := e.recordLocation(v.Location(), freezeZone)
		if err != nil {
			return RecordedValue{}, err
		}
		return RecordedValue{Kind: "time", Text: v.Format(time.RFC3339Nano), Offset: offset, TimeState: state, Location: location}, nil
	case *time.Location:
		if v == nil {
			return RecordedValue{Kind: "nil"}, nil
		}
		location, err := e.recordLocation(v, freezeZone)
		if err != nil {
			return RecordedValue{}, err
		}
		return RecordedValue{Kind: "location", Location: location}, nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]RecordedValue, rv.Len())
		for i := range items {
			item, err := e.recordTypedValue(rv.Index(i).Interface(), freezeZone)
			if err != nil {
				return RecordedValue{}, err
			}
			items[i] = item
		}
		kind := "array"
		if _, ok := value.([][2]any); ok {
			kind = "pairs"
		}
		return RecordedValue{Kind: kind, Items: items}, nil
	case reflect.Map:
		fields := make(map[string]RecordedValue, rv.Len())
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, key := range keys {
			if key.Kind() != reflect.String {
				return RecordedValue{}, fmt.Errorf("unsupported environment map key")
			}
			item, err := e.recordTypedValue(rv.MapIndex(key).Interface(), freezeZone)
			if err != nil {
				return RecordedValue{}, err
			}
			fields[key.String()] = item
		}
		return RecordedValue{Kind: "map", Fields: fields}, nil
	}
	return RecordedValue{}, fmt.Errorf("unsupported environment value")
}

func (e *Evaluation) restoreValue(v RecordedValue) (any, error) {
	switch v.Kind {
	case "nil":
		return nil, nil
	case "bool":
		return strconv.ParseBool(v.Text)
	case "string":
		return v.Text, nil
	case "int":
		return strconv.Atoi(v.Text)
	case "int64":
		return strconv.ParseInt(v.Text, 10, 64)
	case "duration":
		n, err := strconv.ParseInt(v.Text, 10, 64)
		return time.Duration(n), err
	case "float64":
		return strconv.ParseFloat(v.Text, 64)
	case "location":
		return e.restoreLocation(v.Location)
	case "time":
		location, err := e.restoreLocation(v.Location)
		if err != nil {
			return nil, err
		}
		at, err := restoreTimeState(v.TimeState, location)
		if err != nil {
			return nil, err
		}
		_, offset := at.Zone()
		if offset != v.Offset || at.Format(time.RFC3339Nano) != v.Text {
			return nil, fmt.Errorf("recorded billing timezone changed")
		}
		return at, nil
	case "array", "pairs":
		items := make([]any, len(v.Items))
		for i, item := range v.Items {
			restored, err := e.restoreValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = restored
		}
		if v.Kind == "pairs" {
			pairs := make([][2]any, len(items))
			for i, item := range items {
				pair, ok := item.([]any)
				if !ok || len(pair) != 2 {
					return nil, fmt.Errorf("invalid recorded pairs")
				}
				pairs[i] = [2]any{pair[0], pair[1]}
			}
			return pairs, nil
		}
		return items, nil
	case "map":
		fields := make(map[string]any, len(v.Fields))
		for key, item := range v.Fields {
			restored, err := e.restoreValue(item)
			if err != nil {
				return nil, err
			}
			fields[key] = restored
		}
		return fields, nil
	}
	return nil, fmt.Errorf("unknown recorded environment type")
}

func (e *Evaluation) recordLocation(location *time.Location, freeze bool) (*recordedLocation, error) {
	if e.locationIDs == nil {
		e.locationIDs = make(map[*time.Location]int)
		e.locations = make(map[int]*time.Location)
		e.locationDefinitions = make(map[int]recordedLocation)
	}
	id := e.locationIDs[location]
	if id == 0 {
		e.nextLocationID++
		id = e.nextLocationID
		e.locationIDs[location], e.locations[id] = id, location
	}
	definition := recordedLocation{ID: id, Name: location.String(), UTC: location == time.UTC}
	if freeze {
		if known, ok := e.locationDefinitions[id]; ok {
			return &known, nil
		}
		var err error
		definition.Data, err = snapshotLocation(location)
		if err != nil {
			return nil, err
		}
		e.locationDefinitions[id] = definition
	}
	return &definition, nil
}

func (e *Evaluation) restoreLocation(definition *recordedLocation) (*time.Location, error) {
	if definition == nil || definition.ID < 1 {
		return nil, fmt.Errorf("recorded billing timezone identity is missing")
	}
	if e.locationIDs == nil {
		e.locationIDs = make(map[*time.Location]int)
		e.locations = make(map[int]*time.Location)
		e.locationDefinitions = make(map[int]recordedLocation)
	}
	if known, ok := e.locationDefinitions[definition.ID]; ok {
		if !reflect.DeepEqual(known, *definition) {
			return nil, fmt.Errorf("recorded billing timezone identity changed")
		}
		return e.locations[definition.ID], nil
	}
	location := e.locations[definition.ID]
	if location != nil {
		if location.String() != definition.Name || (location == time.UTC) != definition.UTC {
			return nil, fmt.Errorf("recorded billing timezone identity changed")
		}
	} else if definition.UTC {
		if definition.Name != "UTC" || len(definition.Data) != 0 {
			return nil, fmt.Errorf("invalid recorded UTC timezone")
		}
		location = time.UTC
	} else {
		var err error
		location, err = time.LoadLocationFromTZData(definition.Name, definition.Data)
		if err != nil {
			return nil, fmt.Errorf("recorded billing timezone unavailable")
		}
	}
	if otherID := e.locationIDs[location]; otherID != 0 && otherID != definition.ID {
		return nil, fmt.Errorf("recorded billing timezone identity changed")
	}
	e.locationIDs[location], e.locations[definition.ID] = definition.ID, location
	e.locationDefinitions[definition.ID] = *definition
	e.nextLocationID = max(e.nextLocationID, definition.ID)
	return location, nil
}

var replayBuiltins = []string{"now", "date", "timezone", "keys", "values", "toPairs"}

func replayCompileOptions() []expr.Option {
	options := make([]expr.Option, 0, len(replayBuiltins))
	for _, name := range replayBuiltins {
		options = append(options, expr.DisableBuiltin(name))
	}
	return options
}

func builtinCall(name string, args []any) (any, error) {
	for _, function := range builtin.Builtins {
		if function.Name == name {
			return function.Func(args...)
		}
	}
	return nil, fmt.Errorf("unknown billing environment function")
}

func bindEnvironment(env map[string]any, e *Evaluation) {
	env[timeMembersFunction] = e.timeMembers
	env["now"] = func(zones ...*time.Location) (time.Time, error) {
		args := make([]any, len(zones))
		for i, zone := range zones {
			args[i] = zone
		}
		value, err := e.invoke("now", args, func() (any, error) {
			if len(zones) > 1 {
				return nil, fmt.Errorf("invalid now arguments")
			}
			at := time.Now()
			if e != nil && e.Now != nil {
				at = e.Now()
			}
			if len(zones) == 1 {
				at = at.In(zones[0])
			}
			return at, nil
		})
		if err != nil {
			return time.Time{}, err
		}
		return value.(time.Time), nil
	}
	env["timezone"] = func(zone string) (*time.Location, error) {
		value, err := e.invoke("timezone", []any{zone}, func() (any, error) { return time.LoadLocation(zone) })
		if err != nil {
			return nil, err
		}
		return value.(*time.Location), nil
	}
	env["date"] = func(args ...any) (time.Time, error) {
		value, err := e.invoke("date", args, func() (any, error) { return builtinCall("date", args) })
		if err != nil {
			return time.Time{}, err
		}
		return value.(time.Time), nil
	}
	for _, name := range []string{"keys", "values", "toPairs"} {
		env[name] = func(value any) (any, error) {
			return e.invoke(name, []any{value}, func() (any, error) { return builtinCall(name, []any{value}) })
		}
	}
	for _, name := range []string{"hour", "minute", "weekday", "month", "day"} {
		env[name] = func(zone string) (int, error) {
			value, err := e.invoke(name, []any{zone}, func() (any, error) {
				at := timeInZone(zone)
				if e != nil && e.Now != nil {
					at = e.Now()
					location, err := time.LoadLocation(zone)
					if err == nil {
						at = at.In(location)
					} else {
						at = at.UTC()
					}
				}
				switch name {
				case "hour":
					return at.Hour(), nil
				case "minute":
					return at.Minute(), nil
				case "weekday":
					return int(at.Weekday()), nil
				case "month":
					return int(at.Month()), nil
				default:
					return at.Day(), nil
				}
			})
			if err != nil {
				return 0, err
			}
			return value.(int), nil
		}
	}
}

// CloneRequestInput uses the same wire types as persisted evidence. Body bytes
// remain unchanged; Usage retains the float representation used by NewAPI.
func CloneRequestInput(input RequestInput) (RequestInput, error) {
	data, err := common.Marshal(input)
	if err != nil {
		return RequestInput{}, fmt.Errorf("billing request input cannot be encoded")
	}
	var clone RequestInput
	if err = common.Unmarshal(data, &clone); err != nil {
		return RequestInput{}, fmt.Errorf("billing request input cannot be decoded")
	}
	if input.Attributes != nil {
		clone.Attributes, err = FreezeAttributes(input.Attributes)
		if err != nil {
			return RequestInput{}, err
		}
	} else {
		clone.Attributes = nil
	}
	return clone, nil
}
