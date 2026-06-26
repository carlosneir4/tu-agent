package autolink

import (
	"reflect"
	"testing"
)

func TestSymbols(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"pascalcase only", "OrderService calls PaymentGateway", []string{"OrderService", "PaymentGateway"}},
		{"ignores lowercase methods", "getSlug() falls back to value", nil},
		{"ignores short acronyms", "DB and IO are fine but Order matters", []string{"Order"}},
		{"dedups, keeps first-seen order", "Order then Cart then Order", []string{"Order", "Cart"}},
		{"drops stoplist words", "Package uses Jackson for OrderService", []string{"OrderService"}},
		{"min length 3 (Ab excluded)", "Ab Abc", []string{"Abc"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Symbols(tc.content)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Symbols(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestSymbolsStoplistFrameworkTerms(t *testing.T) {
	// React/TS + Go scaffolding words are dropped (rarely the subject of a note,
	// commonly unique class names); a real domain symbol still passes.
	content := "Props State Context Provider Component Element Ref Fragment Children " +
		"Store Handler Server Client Config Service OrderService"
	got := Symbols(content)
	want := []string{"OrderService"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Symbols(framework terms) = %v, want %v", got, want)
	}
}

func TestResolve(t *testing.T) {
	index := map[string]string{
		"OrderService":   "src/order/OrderService.java::OrderService",
		"PaymentGateway": "src/pay/PaymentGateway.java::PaymentGateway",
	}
	got := Resolve([]string{"OrderService", "Unknown", "PaymentGateway", "OrderService"}, index)
	want := []string{
		"src/order/OrderService.java::OrderService",
		"src/pay/PaymentGateway.java::PaymentGateway",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve = %v, want %v", got, want)
	}
}

func TestResolveEmpty(t *testing.T) {
	if got := Resolve(nil, map[string]string{"X": "y"}); len(got) != 0 {
		t.Errorf("Resolve(nil) = %v, want empty", got)
	}
}
