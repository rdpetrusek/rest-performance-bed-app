package bed

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanSSE(t *testing.T) {
	const stream = "event:body\n" +
		"data:{\"present\":true}\n" +
		"\n" +
		"event:time\n" +
		"data:{\"t\":123}\n" +
		"\n" +
		"event:preferences\n" +
		"data:{\"firmness\":28}\n"

	var got []Event
	if err := scanSSE(strings.NewReader(stream), func(e Event) { got = append(got, e) }); err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Name: "body", Data: []byte(`{"present":true}`)},
		{Name: "time", Data: []byte(`{"t":123}`)},
		{Name: "preferences", Data: []byte(`{"firmness":28}`)},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestScanSSEIgnoresDataWithoutEvent(t *testing.T) {
	const stream = "data:{\"orphan\":true}\n" +
		"event:body\n" +
		"data:{\"present\":true}\n"

	var got []Event
	if err := scanSSE(strings.NewReader(stream), func(e Event) { got = append(got, e) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "body" {
		t.Errorf("got %+v, want only the body event", got)
	}
}
