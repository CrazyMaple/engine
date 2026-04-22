package codec

import (
	"testing"
)

type benchMsg struct {
	Type string  `json:"type"`
	ID   int     `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

func BenchmarkJSONCodecEncode(b *testing.B) {
	c := NewJSONCodec()
	msg := &benchMsg{Type: "benchMsg", ID: 1, Name: "player", X: 100.5, Y: 200.3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.Encode(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONCodecDecode(b *testing.B) {
	c := NewJSONCodec()
	c.Register(&benchMsg{})

	data := []byte(`{"type":"benchMsg","id":1,"name":"player","x":100.5,"y":200.3}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.Decode(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONCodecRoundTrip(b *testing.B) {
	c := NewJSONCodec()
	c.Register(&benchMsg{})
	msg := &benchMsg{Type: "benchMsg", ID: 1, Name: "player", X: 100.5, Y: 200.3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := c.Encode(msg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = c.Decode(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSimpleProcessorMarshalUnmarshal(b *testing.B) {
	c := NewJSONCodec()
	c.Register(&benchMsg{})
	p := NewSimpleProcessor(c)

	msg := &benchMsg{Type: "benchMsg", ID: 1, Name: "player", X: 100.5, Y: 200.3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := p.Marshal(msg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = p.Unmarshal(data[0])
		if err != nil {
			b.Fatal(err)
		}
	}
}
