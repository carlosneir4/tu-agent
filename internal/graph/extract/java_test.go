package extract

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

const invoiceSrc = `package com.acme.billing;

import com.acme.core.BaseService;
import com.acme.util.*;
import static org.junit.Assert.assertTrue;

public class InvoiceService extends BaseService implements Auditable {
    public void calculate() {
        Helper.assist();
        this.tally();
    }
    private int tally() { return 0; }
}
`

func TestParseJavaExtractsFacts(t *testing.T) {
	facts, err := ParseJava("src/com/acme/billing/InvoiceService.java", []byte(invoiceSrc))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}
	if facts.Meta.Package != "com.acme.billing" {
		t.Errorf("package = %q", facts.Meta.Package)
	}
	wantImports := []string{"com.acme.core.BaseService", "com.acme.util.*"}
	if len(facts.Meta.Imports) != 2 || facts.Meta.Imports[0] != wantImports[0] || facts.Meta.Imports[1] != wantImports[1] {
		t.Errorf("imports = %v, want %v (static import must be ignored)", facts.Meta.Imports, wantImports)
	}

	byID := map[string]graph.Node{}
	for _, n := range facts.Nodes {
		byID[n.ID] = n
	}
	cls, ok := byID["src/com/acme/billing/InvoiceService.java::InvoiceService"]
	if !ok || cls.Kind != graph.KindClass {
		t.Fatalf("class node missing or wrong kind: %+v", facts.Nodes)
	}
	if cls.Line == 0 || cls.EndLine <= cls.Line {
		t.Errorf("class line range = %d-%d", cls.Line, cls.EndLine)
	}
	if _, ok := byID["src/com/acme/billing/InvoiceService.java::InvoiceService.calculate"]; !ok {
		t.Errorf("method node calculate missing")
	}

	kinds := map[graph.EdgeKind][]string{}
	for _, r := range facts.Refs {
		kinds[r.Kind] = append(kinds[r.Kind], r.Name)
	}
	if len(kinds[graph.EdgeExtends]) != 1 || kinds[graph.EdgeExtends][0] != "BaseService" {
		t.Errorf("extends refs = %v", kinds[graph.EdgeExtends])
	}
	if len(kinds[graph.EdgeImplements]) != 1 || kinds[graph.EdgeImplements][0] != "Auditable" {
		t.Errorf("implements refs = %v", kinds[graph.EdgeImplements])
	}
	calls := kinds[graph.EdgeCalls]
	foundAssist, foundTally := false, false
	for _, c := range calls {
		if c == "assist" {
			foundAssist = true
		}
		if c == "tally" {
			foundTally = true
		}
	}
	if !foundAssist || !foundTally {
		t.Errorf("calls refs = %v, want assist and tally", calls)
	}
	if len(facts.Contains) < 3 { // file→class, class→calculate, class→tally
		t.Errorf("contains edges = %+v", facts.Contains)
	}
}

// TestParseJavaCapturesCallReceiver verifies the call receiver is recorded on
// each EdgeCalls ref, so the resolver can tell LOGGER.error() (a logging call)
// from result.error() (a project method call) — they share the method name.
func TestParseJavaCapturesCallReceiver(t *testing.T) {
	src := `package com.acme.ing;

public class Ingestor {
    private final org.slf4j.Logger LOGGER = null;

    public void run(IngestionResult result, IngestionError err) {
        LOGGER.error("boom");
        result.error(err);
    }
}
`
	facts, err := ParseJava("ing/Ingestor.java", []byte(src))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}
	recvByName := map[string]string{}
	for _, r := range facts.Refs {
		if r.Kind == graph.EdgeCalls {
			recvByName[r.Name+"@"+r.Recv] = r.Recv
		}
	}
	if _, ok := recvByName["error@LOGGER"]; !ok {
		t.Errorf("want a calls ref error with Recv=LOGGER; refs: %+v", facts.Refs)
	}
	if _, ok := recvByName["error@result"]; !ok {
		t.Errorf("want a calls ref error with Recv=result; refs: %+v", facts.Refs)
	}
}

// TestParseJavaDeduplicatesOverloads verifies that overloaded constructors and
// methods (which share the same simple name) produce exactly one node each,
// not one per overload. This is the root cause of the UNIQUE constraint
// failure seen with exception classes that have multiple constructors.
func TestParseJavaDeduplicatesOverloads(t *testing.T) {
	src := `package com.acme.exceptions;

public class AuthException extends RuntimeException {
    public AuthException() { super("auth required"); }
    public AuthException(String message) { super(message); }
    public AuthException(String message, Throwable cause) { super(message, cause); }

    public static AuthException wrap(Throwable cause) { return new AuthException("wrapped", cause); }
    public static AuthException wrap(String msg) { return new AuthException(msg); }
}
`
	facts, err := ParseJava("src/com/acme/exceptions/AuthException.java", []byte(src))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}

	seen := map[string]int{}
	for _, n := range facts.Nodes {
		seen[n.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate node ID %q appears %d times", id, count)
		}
	}

	ctorID := "src/com/acme/exceptions/AuthException.java::AuthException.AuthException"
	if seen[ctorID] != 1 {
		t.Errorf("constructor node %q: want 1 occurrence, got %d", ctorID, seen[ctorID])
	}
	wrapID := "src/com/acme/exceptions/AuthException.java::AuthException.wrap"
	if seen[wrapID] != 1 {
		t.Errorf("overloaded method node %q: want 1 occurrence, got %d", wrapID, seen[wrapID])
	}
}

func TestParseJavaAutowiredField(t *testing.T) {
	src := `package com.acme.app;

public class OrderController {
    @Autowired
    private OrderService orderService;

    private int counter;
}
`
	facts, err := ParseJava("app/OrderController.java", []byte(src))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}

	var foundInject, foundCounter bool
	for _, r := range facts.Refs {
		if r.Kind == graph.EdgeImports && r.Name == "OrderService" &&
			r.FromID == "app/OrderController.java::OrderController" {
			foundInject = true
		}
		// A non-@Autowired field (int counter) must NOT emit an import ref.
		if r.Kind == graph.EdgeImports && r.Name == "int" {
			foundCounter = true
		}
	}
	if !foundInject {
		t.Errorf("expected EdgeImports ref to OrderService from the class node; refs: %+v", facts.Refs)
	}
	if foundCounter {
		t.Errorf("non-@Autowired field must not emit an import ref; refs: %+v", facts.Refs)
	}
}

func TestParseJavaDetectsTests(t *testing.T) {
	src := `package com.acme.billing;
import org.junit.Test;
public class InvoiceServiceTest {
    public void testCalculate() {}
}
`
	facts, err := ParseJava("src/com/acme/billing/InvoiceServiceTest.java", []byte(src))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}
	var found bool
	for _, n := range facts.Nodes {
		if n.Name == "InvoiceServiceTest" && n.Kind == graph.KindTest {
			found = true
		}
	}
	if !found {
		t.Errorf("test class not marked kind=test: %+v", facts.Nodes)
	}
}

const signatureJavaSrc = `package com.acme.billing;

public class InvoiceService {
    public InvoiceService(String region) {}
    public int process(Invoice invoice,
                       boolean force) { return 0; }
    public void close() {}
}
`

func TestParseJavaExtractsSignatures(t *testing.T) {
	facts, err := ParseJava("src/InvoiceService.java", []byte(signatureJavaSrc))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}
	byID := map[string]graph.Node{}
	for _, n := range facts.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id, params, ret string
	}{
		{"src/InvoiceService.java::InvoiceService.InvoiceService", "(String region)", ""},
		{"src/InvoiceService.java::InvoiceService.process", "(Invoice invoice, boolean force)", "int"},
		{"src/InvoiceService.java::InvoiceService.close", "()", "void"},
	}
	for _, tc := range tests {
		n, ok := byID[tc.id]
		if !ok {
			t.Errorf("node %s missing", tc.id)
			continue
		}
		if n.Params != tc.params || n.ReturnType != tc.ret {
			t.Errorf("%s signature = %q / %q, want %q / %q", tc.id, n.Params, n.ReturnType, tc.params, tc.ret)
		}
	}
}

func TestParseJava_exportedFlag(t *testing.T) {
	src := `package p;

public class Svc {
    public int run() { return 1; }
    private int helper() { return 2; }
    int pkgLocal() { return 3; }
    protected int prot() { return 4; }
}

interface Api {
    int call();
}
`
	f, err := ParseJava("p/Svc.java", []byte(src))
	if err != nil {
		t.Fatalf("ParseJava: %v", err)
	}
	want := map[string]bool{
		"p/Svc.java::Svc.run":      true,
		"p/Svc.java::Svc.helper":   false,
		"p/Svc.java::Svc.pkgLocal": false,
		"p/Svc.java::Svc.prot":     false,
		"p/Svc.java::Api.call":     true, // interface methods are implicitly public
	}
	got := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Kind == graph.KindFunction {
			got[n.ID] = n.Exported
		}
	}
	for id, exp := range want {
		if got[id] != exp {
			t.Errorf("node %q Exported = %v, want %v", id, got[id], exp)
		}
	}
}
