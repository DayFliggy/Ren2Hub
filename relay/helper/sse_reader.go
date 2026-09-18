package helper

import "io"

// ReadSSEEvents shares the streaming decoder without writing SSE headers or
// heartbeats. The upstream response body retains its request cancellation and
// non-streaming completion deadline.
func ReadSSEEvents(reader io.Reader, consume func(data, eventName string) (bool, error)) error {
	scanner := NewStreamScanner(reader)
	scanner.Split(splitSSELine)
	decoder := sseDecoder{maxSize: getScannerBufferSize()}
	for scanner.Scan() {
		event, err := decoder.addLine(scanner.Text())
		if err != nil {
			return err
		}
		if event == nil {
			continue
		}
		stop, err := consume(event.data, event.name)
		if err != nil || stop {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if event := decoder.finish(); event != nil {
		_, err := consume(event.data, event.name)
		return err
	}
	return nil
}
