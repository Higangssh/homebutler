package capability

import "testing"

// Every write says whether one call can delete or overwrite something, and
// nothing else says it. A new write tool fails here until somebody has looked
// at what it does and decided, which is the point: left unset it would go out
// annotated destructive, and a decision nobody made is not one a client should
// be shown.
func TestEveryWriteSaysWhetherItErases(t *testing.T) {
	for _, c := range Registry {
		switch {
		case c.Risk == RiskWrite && c.Erases != ErasesNothing && c.Erases != ErasesSomething:
			t.Errorf("%s is a write and has not said whether it erases anything (Erases is %q); set ErasesNothing or ErasesSomething from what the call does", c.Tool.Name, c.Erases)
		case c.Risk != RiskWrite && c.Erases != "":
			t.Errorf("%s is classed %s and sets Erases %q; only a write has a second answer", c.Tool.Name, c.Risk, c.Erases)
		}
	}
}

// A class without hints would go out as the fallback, which says "may
// destroy". That is the safe answer for a value the server cannot place, and
// the wrong one for a class somebody added on purpose: a client would ask
// before every call to a tool that only reads.
func TestEveryRiskClassHasAnnotations(t *testing.T) {
	known := map[Risk]bool{}
	for _, risk := range Risks() {
		known[risk] = true
		for _, erases := range []Erasure{ErasesNothing, ErasesSomething} {
			if _, ok := annotationsFor(risk, erases); !ok {
				t.Errorf("class %q has no MCP annotations; annotationsFor falls back to destructive for it", risk)
				break
			}
		}
	}
	for _, c := range Registry {
		if !known[c.Risk] {
			t.Errorf("%s is classed %q, which is not in Risks()", c.Tool.Name, c.Risk)
		}
	}
}

// The hints may say more than the class, never less.
func TestNoToolIsAnnotatedLessCautiousThanItsClass(t *testing.T) {
	for _, c := range Registry {
		a := c.Annotations()
		if c.Risk != RiskRead && a.ReadOnlyHint {
			t.Errorf("%s is classed %s and annotated read-only", c.Tool.Name, c.Risk)
		}
		if c.Risk == RiskDestructive && !a.DestructiveHint {
			t.Errorf("%s is destructive and annotated not destructive", c.Tool.Name)
		}
	}
}

func TestAnUndecidedOrUnknownClassIsAnnotatedAsDestructive(t *testing.T) {
	got, ok := annotationsFor("", "")
	if ok {
		t.Fatal("an empty class must not count as mapped")
	}
	if got.ReadOnlyHint || !got.DestructiveHint {
		t.Errorf("an unknown class is annotated %+v; it has to fail closed, like --max-risk does", got)
	}
	if got, _ := annotationsFor(RiskWrite, ""); !got.DestructiveHint {
		t.Errorf("a write that has not said whether it erases is annotated %+v; it has to be destructive", got)
	}
}
