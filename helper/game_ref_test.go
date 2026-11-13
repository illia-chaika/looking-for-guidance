package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDetailsInferProvenanceWithoutChangingExplicitRequests(t *testing.T) {
	s := parseGameSnapshot(gearCards, 0)
	test := func(input, expected gameRef) {
		t.Helper()
		result, err := input.resolve(s)
		if err != nil || result != expected {
			t.Fatalf("resolve(%+v) = %+v, %v; want %+v", input, result, err, expected)
		}
	}
	test(gameRef{Kind: "item", ID: 100, Link: "item:100:11"}, gameRef{Kind: "item", ID: 100, Link: "item:100:11", Section: "gear"})
	test(gameRef{Kind: "item", ID: 100, Link: "item:100:22"}, gameRef{Kind: "item", ID: 100, Link: "item:100:22", Section: "references"})
	baseItem := gameRef{Kind: "item", ID: 100, Section: "gear"}
	test(baseItem, baseItem)
	if _, err := (gameRef{Kind: "item", ID: 100}).resolve(s); err == nil || !strings.Contains(err.Error(), "multiple variants") {
		t.Fatalf("ambiguous item did not request an exact link: %v", err)
	}
	delete(s.Sections, "gear")
	test(gameRef{Kind: "item", ID: 100}, gameRef{Kind: "item", ID: 100, Link: "item:100:22", Section: "references"})
	if _, err := (gameRef{Kind: "item", ID: 100, Link: "item:100:11"}).resolve(s); err == nil {
		t.Fatal("captured item from a disabled section became visible")
	}
	spell := gameRef{Kind: "spell", ID: 42, Section: "spells"}
	test(spell, spell)
	s.Entries = append(s.Entries, gameEntry{"section": "spells", "kind": "spell", "id": float64(42)})
	test(gameRef{Kind: "spell", ID: 42}, spell)
}

func TestDetailsInvalidBatchReportsFieldBeforeEnqueuing(t *testing.T) {
	dir := t.TempDir()
	id := newCatalogQuestion(t, dir)
	for _, test := range []struct{ refs, want string }{
		{"[]", "1 to 6"},
		{"[" + strings.Repeat("{},", 6) + "{}]", "got 7"},
		{`[{"kind":"item","id":100,"section":"gear"},{"kind":"faction","id":7}]`, "refs[1]: kind"},
		{`[{"kind":"item","id":2147483648}]`, "refs[0]: id"},
		{`[{"kind":"item","id":100,"section":"spells"}]`, "gear, bags, or references"},
		{`[{"kind":"item","id":100,"link":"item:77"}]`, "matching kind and id"},
		{`[{"kind":"spell","id":42}]`, "outside candidate"},
		{`[{"kind":"item","id":100,"section":"bags"}]`, "disabled or unavailable"},
		{`[{"kind":"spell","id":42,"section":"references"}]`, "no matching player-attached reference"},
	} {
		_, err := queryDetails(dir, json.RawMessage(`{"snapshot_id":"`+id+`","refs":`+test.refs+`}`))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v, want %q", test.refs, err, test.want)
		}
	}
	if _, err := os.Stat(detailDir(dir)); !os.IsNotExist(err) {
		t.Fatal("invalid lookup accessed the game bridge", err)
	}
}
