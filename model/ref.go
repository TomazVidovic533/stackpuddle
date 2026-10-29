package model

import (
	"regexp"
	"strings"
)

// ObjectKind is the kind of a declared object.
type ObjectKind int

const (
	ObjectWorkload ObjectKind = iota + 1
	ObjectModule
	ObjectClient
)

// String returns the kind's reference prefix: "workload", "module" or "client".
func (k ObjectKind) String() string {
	switch k {
	case ObjectWorkload:
		return "workload"
	case ObjectModule:
		return "module"
	case ObjectClient:
		return "client"
	default:
		return "unknown"
	}
}

// context returns where a consumer of this kind runs: workloads and module instances in a
// container, clients on the host.
func (k ObjectKind) context() Context {
	if k == ObjectClient {
		return ContextHost
	}
	return ContextContainer
}

func objectKinds() []ObjectKind {
	return []ObjectKind{ObjectWorkload, ObjectModule, ObjectClient}
}

// envelopeKind returns the kind a Model's object envelope names: "Workload", "ModuleInstance" or
// "Client".
func (k ObjectKind) envelopeKind() string {
	switch k {
	case ObjectWorkload:
		return "Workload"
	case ObjectModule:
		return "ModuleInstance"
	case ObjectClient:
		return "Client"
	default:
		return "unknown"
	}
}

// parseClosed returns the member of set that spell writes as text. Each closed set is spelled once,
// by the method that encodes it, and parsed through that same method.
func parseClosed[T any](set []T, spell func(T) string, text string) (T, bool) {
	for _, member := range set {
		if spell(member) == text {
			return member, true
		}
	}
	var none T
	return none, false
}

// ObjectRef identifies a declared object, written "<kind>/<key>", such as "workload/api".
// It is comparable and works as a map key.
type ObjectRef struct {
	kind ObjectKind
	key  ObjectKey
}

// ParseObjectRef returns s as an ObjectRef, or an *Error with code reference.invalid_ref.
func ParseObjectRef(s string) (ObjectRef, error) {
	prefix, rest, found := strings.Cut(s, "/")
	if !found {
		return ObjectRef{}, invalidRef("object reference %q needs <kind>/<key>", s)
	}
	for _, kind := range objectKinds() {
		if kind.String() != prefix {
			continue
		}
		key, err := ParseObjectKey(rest)
		if err != nil {
			return ObjectRef{}, invalidRef("object reference %q has an invalid key", s)
		}
		return ObjectRef{kind: kind, key: key}, nil
	}
	return ObjectRef{}, invalidRef("object reference %q must start with workload/, module/ or client/", s)
}

// Kind returns the object's kind.
func (r ObjectRef) Kind() ObjectKind { return r.kind }

// Key returns the object's key.
func (r ObjectRef) Key() ObjectKey { return r.key }

// String returns "<kind>/<key>".
func (r ObjectRef) String() string { return r.kind.String() + "/" + r.key.String() }

// ExportRef names one Export of one object. It is comparable and works as a map key.
type ExportRef struct {
	Object ObjectRef
	Export ExportKey
}

// BindingRef is a binding's identity: the consumer and the consumer's Dependency key. No binding
// has a name, so changing a binding's source keeps its identity.
type BindingRef struct {
	Consumer   ObjectRef
	Dependency DependencyKey
}

// String returns "binding/<consumer>/<dependency>", such as "binding/workload/api/database".
func (r BindingRef) String() string {
	return "binding/" + r.Consumer.String() + "/" + r.Dependency.String()
}

// RefKind is the kind of a command-line reference.
type RefKind int

const (
	RefWorkload RefKind = iota + 1
	RefModule
	RefClient
	RefApp
	RefEnv
	RefResource
	RefOperation
	RefBinding
)

// String returns the reference prefix, such as "app".
func (k RefKind) String() string {
	switch k {
	case RefWorkload:
		return "workload"
	case RefModule:
		return "module"
	case RefClient:
		return "client"
	case RefApp:
		return "app"
	case RefEnv:
		return "env"
	case RefResource:
		return "resource"
	case RefOperation:
		return "operation"
	case RefBinding:
		return "binding"
	default:
		return "unknown"
	}
}

func refKinds() []RefKind {
	return []RefKind{RefWorkload, RefModule, RefClient, RefApp, RefEnv, RefResource, RefOperation, RefBinding}
}

// Ref is one positional reference of the command-line grammar. It is comparable and works as a map
// key; two Refs are equal exactly when their spellings are.
type Ref struct {
	kind RefKind
	text string
}

const (
	resourceSegments    = 3
	bindingSegments     = 3
	resourcePartMax     = 63
	operationIDPrefix   = "op-"
	operationIDLength   = 26
	crockfordBase32Char = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

var (
	resourceTypeName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	resourceItemName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// ParseRef parses one reference: workload/<key>, module/<key>, client/<key>, app/<key>,
// env/<name>, resource/<instance>/<type>/<name>, operation/op-<26 Crockford base32 characters> or
// binding/<consumer kind>/<consumer key>/<dependency key>. It returns an *Error with code
// reference.invalid_ref for anything else. Resource types and names are checked for spelling only;
// what they mean belongs to the module.
func ParseRef(s string) (Ref, error) {
	prefix, rest, found := strings.Cut(s, "/")
	if !found {
		return Ref{}, invalidRef("reference %q needs <kind>/<name>", s)
	}
	for _, kind := range refKinds() {
		if kind.String() == prefix {
			if err := checkRefBody(kind, rest); err != nil {
				return Ref{}, invalidRef("reference %q is not valid: %s", s, err.Error())
			}
			return Ref{kind: kind, text: s}, nil
		}
	}
	return Ref{}, invalidRef("reference %q has an unknown kind; use workload, module, client, app, env, resource, operation or binding", s)
}

func checkRefBody(kind RefKind, body string) error {
	switch kind {
	case RefWorkload, RefModule, RefClient, RefApp:
		_, err := ParseObjectKey(body)
		return err
	case RefEnv:
		_, err := ParseEnvironmentName(body)
		return err
	case RefResource:
		return checkResourceBody(body)
	case RefOperation:
		return checkOperationID(body)
	case RefBinding:
		_, err := parseBindingBody(body)
		return err
	default:
		return invalidRef("unknown reference kind")
	}
}

func checkResourceBody(body string) error {
	parts := strings.Split(body, "/")
	if len(parts) != resourceSegments {
		return invalidRef("a resource reference needs <instance>/<type>/<name>")
	}
	if _, err := ParseObjectKey(parts[0]); err != nil {
		return err
	}
	if len(parts[1]) > resourcePartMax || !resourceTypeName.MatchString(parts[1]) {
		return invalidRef("resource type must match %s, at most %d characters", resourceTypeName, resourcePartMax)
	}
	if len(parts[2]) > resourcePartMax || !resourceItemName.MatchString(parts[2]) {
		return invalidRef("resource name must match %s, at most %d characters", resourceItemName, resourcePartMax)
	}
	return nil
}

func checkOperationID(body string) error {
	id, found := strings.CutPrefix(body, operationIDPrefix)
	if !found || len(id) != operationIDLength {
		return invalidRef("an operation ID is %s followed by %d Crockford base32 characters", operationIDPrefix, operationIDLength)
	}
	for _, ch := range id {
		if !strings.ContainsRune(crockfordBase32Char, ch) {
			return invalidRef("an operation ID uses the uppercase Crockford base32 alphabet")
		}
	}
	return nil
}

// parseBindingBody reads "<consumer kind>/<consumer key>/<dependency key>". The last segment is
// always the Dependency key, so the spelling is unambiguous.
func parseBindingBody(body string) (BindingRef, error) {
	parts := strings.Split(body, "/")
	if len(parts) != bindingSegments {
		return BindingRef{}, invalidRef("a binding reference needs <consumer kind>/<consumer key>/<dependency key>")
	}
	consumer, err := ParseObjectRef(parts[0] + "/" + parts[1])
	if err != nil {
		return BindingRef{}, err
	}
	dependency, err := ParseDependencyKey(parts[2])
	if err != nil {
		return BindingRef{}, err
	}
	return BindingRef{Consumer: consumer, Dependency: dependency}, nil
}

// Kind returns the reference's kind.
func (r Ref) Kind() RefKind { return r.kind }

// String returns the reference as written.
func (r Ref) String() string { return r.text }

// refOf returns the Ref of a declared object.
func refOf(object ObjectRef) Ref {
	kinds := map[ObjectKind]RefKind{ObjectWorkload: RefWorkload, ObjectModule: RefModule, ObjectClient: RefClient}
	return Ref{kind: kinds[object.kind], text: object.String()}
}

func appRef(key ObjectKey) Ref { return Ref{kind: RefApp, text: RefApp.String() + "/" + key.String()} }

func envRef(name EnvironmentName) Ref {
	return Ref{kind: RefEnv, text: RefEnv.String() + "/" + name.String()}
}

func bindingRef(ref BindingRef) Ref { return Ref{kind: RefBinding, text: ref.String()} }
