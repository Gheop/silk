package fidelity

import "testing"

const redRect = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="red"/></svg>`
const blueRect = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="blue"/></svg>`

// Same geometry written differently: must be within tolerance.
const redRectPath = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0h10v10H0z" fill="red"/></svg>`

func TestIdenticalPasses(t *testing.T) {
	if ResvgPath() == "" {
		t.Skip("resvg not installed")
	}
	res, err := RenderDiff(t.TempDir(), []byte(redRect), []byte(redRect))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Acceptable() || res.MaxDiff != 0 {
		t.Errorf("identical renders differ: %s", res)
	}
}

func TestEquivalentGeometryPasses(t *testing.T) {
	if ResvgPath() == "" {
		t.Skip("resvg not installed")
	}
	res, err := RenderDiff(t.TempDir(), []byte(redRect), []byte(redRectPath))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Acceptable() {
		t.Errorf("equivalent renders differ: %s", res)
	}
}

func TestDifferentFails(t *testing.T) {
	if ResvgPath() == "" {
		t.Skip("resvg not installed")
	}
	res, err := RenderDiff(t.TempDir(), []byte(redRect), []byte(blueRect))
	if err != nil {
		t.Fatal(err)
	}
	if res.Acceptable() {
		t.Errorf("red vs blue accepted: %s", res)
	}
}
