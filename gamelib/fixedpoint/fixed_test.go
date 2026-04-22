package fixedpoint

import (
	"math"
	"testing"
)

func TestFromIntToInt(t *testing.T) {
	cases := []int{0, 1, -1, 100, -100, 32767, -32768}
	for _, v := range cases {
		f := FromInt(v)
		got := f.ToInt()
		if got != v {
			t.Errorf("FromInt(%d).ToInt() = %d, want %d", v, got, v)
		}
	}
}

func TestFromFloat64RoundTrip(t *testing.T) {
	cases := []float64{0, 1, -1, 0.5, -0.5, 3.14159, -100.25, 0.000015}
	for _, v := range cases {
		f := FromFloat64(v)
		got := f.ToFloat64()
		if math.Abs(got-v) > 0.00002 {
			t.Errorf("FromFloat64(%f).ToFloat64() = %f, diff = %e", v, got, math.Abs(got-v))
		}
	}
}

func TestArithmetic(t *testing.T) {
	a := FromFloat64(3.5)
	b := FromFloat64(2.25)

	// Add
	sum := a.Add(b).ToFloat64()
	if math.Abs(sum-5.75) > 0.0001 {
		t.Errorf("3.5 + 2.25 = %f, want 5.75", sum)
	}

	// Sub
	diff := a.Sub(b).ToFloat64()
	if math.Abs(diff-1.25) > 0.0001 {
		t.Errorf("3.5 - 2.25 = %f, want 1.25", diff)
	}

	// Mul
	prod := a.Mul(b).ToFloat64()
	if math.Abs(prod-7.875) > 0.001 {
		t.Errorf("3.5 * 2.25 = %f, want 7.875", prod)
	}

	// Div
	quot := a.Div(b).ToFloat64()
	expected := 3.5 / 2.25
	if math.Abs(quot-expected) > 0.001 {
		t.Errorf("3.5 / 2.25 = %f, want %f", quot, expected)
	}
}

func TestDivByZero(t *testing.T) {
	pos := FromInt(5).Div(Zero)
	if pos != maxFixed {
		t.Errorf("5/0 = %d, want maxFixed", pos)
	}
	neg := FromInt(-5).Div(Zero)
	if neg != minFixed {
		t.Errorf("-5/0 = %d, want minFixed", neg)
	}
}

func TestFloorCeilRound(t *testing.T) {
	v := FromFloat64(3.7)
	if v.Floor().ToInt() != 3 {
		t.Errorf("Floor(3.7) = %d, want 3", v.Floor().ToInt())
	}
	if v.Ceil().ToInt() != 4 {
		t.Errorf("Ceil(3.7) = %d, want 4", v.Ceil().ToInt())
	}
	if v.Round().ToInt() != 4 {
		t.Errorf("Round(3.7) = %d, want 4", v.Round().ToInt())
	}

	v2 := FromFloat64(3.3)
	if v2.Round().ToInt() != 3 {
		t.Errorf("Round(3.3) = %d, want 3", v2.Round().ToInt())
	}

	neg := FromFloat64(-2.7)
	if neg.Floor().ToInt() != -3 {
		t.Errorf("Floor(-2.7) = %d, want -3", neg.Floor().ToInt())
	}
}

func TestMinMaxClamp(t *testing.T) {
	a, b := FromInt(3), FromInt(7)
	if Min(a, b) != a {
		t.Error("Min(3,7) should be 3")
	}
	if Max(a, b) != b {
		t.Error("Max(3,7) should be 7")
	}
	if Clamp(FromInt(10), a, b) != b {
		t.Error("Clamp(10, 3, 7) should be 7")
	}
	if Clamp(FromInt(1), a, b) != a {
		t.Error("Clamp(1, 3, 7) should be 3")
	}
	if Clamp(FromInt(5), a, b) != FromInt(5) {
		t.Error("Clamp(5, 3, 7) should be 5")
	}
}

func TestLerp(t *testing.T) {
	a := FromInt(0)
	b := FromInt(10)
	mid := Lerp(a, b, Half)
	if math.Abs(mid.ToFloat64()-5.0) > 0.001 {
		t.Errorf("Lerp(0, 10, 0.5) = %f, want 5.0", mid.ToFloat64())
	}
}

func TestSinCos(t *testing.T) {
	cases := []struct {
		angle    float64
		wantSin  float64
		wantCos  float64
		epsilon  float64
	}{
		{0, 0, 1, 0.002},
		{math.Pi / 6, 0.5, 0.866025, 0.002},
		{math.Pi / 4, 0.707107, 0.707107, 0.002},
		{math.Pi / 2, 1, 0, 0.002},
		{math.Pi, 0, -1, 0.003},
		{3 * math.Pi / 2, -1, 0, 0.003},
		{-math.Pi / 2, -1, 0, 0.003},
	}

	for _, tc := range cases {
		angle := FromFloat64(tc.angle)
		gotSin := Sin(angle).ToFloat64()
		gotCos := Cos(angle).ToFloat64()
		if math.Abs(gotSin-tc.wantSin) > tc.epsilon {
			t.Errorf("Sin(%f) = %f, want %f (diff=%e)", tc.angle, gotSin, tc.wantSin, math.Abs(gotSin-tc.wantSin))
		}
		if math.Abs(gotCos-tc.wantCos) > tc.epsilon {
			t.Errorf("Cos(%f) = %f, want %f (diff=%e)", tc.angle, gotCos, tc.wantCos, math.Abs(gotCos-tc.wantCos))
		}
	}
}

func TestSqrt(t *testing.T) {
	cases := []struct {
		input float64
		want  float64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{2, 1.41421},
		{100, 10},
		{0.25, 0.5},
	}
	for _, tc := range cases {
		got := Sqrt(FromFloat64(tc.input)).ToFloat64()
		if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("Sqrt(%f) = %f, want %f", tc.input, got, tc.want)
		}
	}
}

func TestAtan2(t *testing.T) {
	cases := []struct {
		y, x float64
		want float64
	}{
		{0, 1, 0},
		{1, 0, math.Pi / 2},
		{0, -1, math.Pi},
		{-1, 0, -math.Pi / 2},
		{1, 1, math.Pi / 4},
	}
	for _, tc := range cases {
		got := Atan2(FromFloat64(tc.y), FromFloat64(tc.x)).ToFloat64()
		if math.Abs(got-tc.want) > 0.02 {
			t.Errorf("Atan2(%f, %f) = %f, want %f", tc.y, tc.x, got, tc.want)
		}
	}
}

func TestVec2Basic(t *testing.T) {
	a := Vec2FromFloat64(3, 4)
	b := Vec2FromFloat64(1, 2)

	// Add
	sum := a.Add(b)
	if math.Abs(sum.X.ToFloat64()-4) > 0.001 || math.Abs(sum.Y.ToFloat64()-6) > 0.001 {
		t.Errorf("Vec2 Add: got %s, want (4, 6)", sum)
	}

	// Sub
	diff := a.Sub(b)
	if math.Abs(diff.X.ToFloat64()-2) > 0.001 || math.Abs(diff.Y.ToFloat64()-2) > 0.001 {
		t.Errorf("Vec2 Sub: got %s, want (2, 2)", diff)
	}

	// Dot
	dot := a.Dot(b)
	if math.Abs(dot.ToFloat64()-11) > 0.01 {
		t.Errorf("Vec2 Dot: got %f, want 11", dot.ToFloat64())
	}

	// Cross
	cross := a.Cross(b)
	if math.Abs(cross.ToFloat64()-2) > 0.01 {
		t.Errorf("Vec2 Cross: got %f, want 2", cross.ToFloat64())
	}
}

func TestVec2Length(t *testing.T) {
	v := Vec2FromFloat64(3, 4)
	length := v.Len().ToFloat64()
	if math.Abs(length-5) > 0.01 {
		t.Errorf("Vec2(3,4).Len() = %f, want 5", length)
	}
}

func TestVec2Normalize(t *testing.T) {
	v := Vec2FromFloat64(3, 4)
	n := v.Normalize()
	length := n.Len().ToFloat64()
	if math.Abs(length-1.0) > 0.02 {
		t.Errorf("Normalized length = %f, want 1.0", length)
	}
}

func TestVec2Distance(t *testing.T) {
	a := Vec2FromFloat64(0, 0)
	b := Vec2FromFloat64(3, 4)
	dist := a.Distance(b).ToFloat64()
	if math.Abs(dist-5) > 0.01 {
		t.Errorf("Distance = %f, want 5", dist)
	}
}

func TestVec2Rotate(t *testing.T) {
	v := Vec2FromFloat64(1, 0)
	rotated := v.Rotate(PiDiv2) // 旋转 90 度
	if math.Abs(rotated.X.ToFloat64()) > 0.01 || math.Abs(rotated.Y.ToFloat64()-1) > 0.01 {
		t.Errorf("Rotate(1,0) by π/2 = %s, want (0, 1)", rotated)
	}
}

// 基准测试

func BenchmarkFixedAdd(b *testing.B) {
	a := FromFloat64(3.5)
	c := FromFloat64(2.25)
	for i := 0; i < b.N; i++ {
		_ = a.Add(c)
	}
}

func BenchmarkFloat64Add(b *testing.B) {
	a := 3.5
	c := 2.25
	for i := 0; i < b.N; i++ {
		_ = a + c
	}
}

func BenchmarkFixedMul(b *testing.B) {
	a := FromFloat64(3.5)
	c := FromFloat64(2.25)
	for i := 0; i < b.N; i++ {
		_ = a.Mul(c)
	}
}

func BenchmarkFloat64Mul(b *testing.B) {
	a := 3.5
	c := 2.25
	for i := 0; i < b.N; i++ {
		_ = a * c
	}
}

func BenchmarkFixedDiv(b *testing.B) {
	a := FromFloat64(3.5)
	c := FromFloat64(2.25)
	for i := 0; i < b.N; i++ {
		_ = a.Div(c)
	}
}

func BenchmarkFloat64Div(b *testing.B) {
	a := 3.5
	c := 2.25
	for i := 0; i < b.N; i++ {
		_ = a / c
	}
}

func BenchmarkFixedSin(b *testing.B) {
	angle := FromFloat64(1.234)
	for i := 0; i < b.N; i++ {
		_ = Sin(angle)
	}
}

func BenchmarkFloat64Sin(b *testing.B) {
	angle := 1.234
	for i := 0; i < b.N; i++ {
		_ = math.Sin(angle)
	}
}

func BenchmarkFixedSqrt(b *testing.B) {
	v := FromFloat64(123.456)
	for i := 0; i < b.N; i++ {
		_ = Sqrt(v)
	}
}

func BenchmarkFloat64Sqrt(b *testing.B) {
	v := 123.456
	for i := 0; i < b.N; i++ {
		_ = math.Sqrt(v)
	}
}

func BenchmarkVec2Normalize(b *testing.B) {
	v := Vec2FromFloat64(3, 4)
	for i := 0; i < b.N; i++ {
		_ = v.Normalize()
	}
}

func BenchmarkVec2Distance(b *testing.B) {
	a := Vec2FromFloat64(1, 2)
	c := Vec2FromFloat64(4, 6)
	for i := 0; i < b.N; i++ {
		_ = a.Distance(c)
	}
}
