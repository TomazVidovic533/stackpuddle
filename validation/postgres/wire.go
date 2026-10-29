package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// options is the part of the module's option schema the rules read. Members the rules never read
// are json.RawMessage so they still decode: a property the schema declares and this struct lacks
// fails DisallowUnknownFields, and the contract tests set every schema property to prove none does.
// The schema is the contract (modules/postgres/content/configuration/schema.json); this struct is
// tested against it, not a second copy of it.
type options struct {
	Users                 map[string]user     `json:"users"`
	Roles                 map[string]role     `json:"roles"`
	Databases             map[string]database `json:"databases"`
	Resources             json.RawMessage     `json:"resources"`
	HostAccess            json.RawMessage     `json:"hostAccess"`
	StartupTimeoutSeconds json.RawMessage     `json:"startupTimeoutSeconds"`
	Settings              json.RawMessage     `json:"settings"`
}

type user struct {
	Purpose           string          `json:"purpose"`
	PasswordSecretRef json.RawMessage `json:"passwordSecretRef"`
	MemberOf          []string        `json:"memberOf"`
}

type role struct {
	Login json.RawMessage `json:"login"`
}

type database struct {
	ConnectionLimit json.RawMessage            `json:"connectionLimit"`
	Settings        json.RawMessage            `json:"settings"`
	Owner           string                     `json:"owner"`
	ConnectRoles    []string                   `json:"connectRoles"`
	Schemas         map[string]schema          `json:"schemas"`
	UserSettings    map[string]json.RawMessage `json:"userSettings"`
}

type schema struct {
	Grants map[string]json.RawMessage `json:"grants"`
}

// The purposes the module's schema allows.
const purposeMigrations = "migrations"

var errTrailingData = errors.New("trailing data after the options object")

// decode reads raw into options, refusing members the struct lacks and data after the object.
func decode(raw json.RawMessage) (options, error) {
	var decoded options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return options{}, fmt.Errorf("decode postgres options: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return options{}, fmt.Errorf("decode postgres options: %w", errTrailingData)
	}
	return decoded, nil
}
