package postgres

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/model"
)

// Validate checks options, the effective options of one postgres instance that already satisfy the
// module's option schema, and returns one diagnostic per violation. Pointers are relative to
// options and carry no file or position, as ModuleDefinition.ValidateOptions reports them; a caller
// places them with model.Diagnostic.At. The order is deterministic and sorted. Messages name
// pointers and map keys, never a value written in options.
//
// The rules (a user has access to a database when it owns it or is a member of one of its
// connectRoles):
//
//  1. a name is not both a user and a role;
//  2. every memberOf, connectRoles and grant name is a declared role, and every database owner and
//     userSettings key is a declared user;
//  3. an owner is a migration user, and one migration user owns at most one database;
//  4. every grantee of a database's schemas is among that database's connectRoles;
//  5. every userSettings key names a user with access to that database.
//
// A rule that depends on a name that failed rule 2 is not reported for it, so each fault has one
// root cause. Shared runtime users and several databases are valid. The error is non-nil only when
// options do not decode, which means the schema precondition did not hold.
func Validate(raw json.RawMessage) (model.Diagnostics, error) {
	decoded, err := decode(raw)
	if err != nil {
		return nil, err
	}
	found := &findings{}
	checkSharedNames(found, decoded)
	checkMemberships(found, decoded)
	for _, name := range slices.Sorted(maps.Keys(decoded.Databases)) {
		checkDatabase(found, decoded, name)
	}
	checkOwnership(found, decoded)
	if found.err != nil {
		return nil, found.err
	}
	return found.list.Sorted(), nil
}

// findings collects diagnostics; the first constructor refusal, which is a programming fault, is kept.
type findings struct {
	list model.Diagnostics
	err  error
}

func (f *findings) add(code model.Code, message string, tokens ...string) {
	diagnostic, err := model.NewModuleDiagnostic(code, model.NewLocation("", pointer(tokens...), 0, 0), message)
	if err != nil {
		f.err = err
		return
	}
	f.list = append(f.list, diagnostic)
}

// pointer joins tokens into a JSON pointer, escaping "~" and "/".
func pointer(tokens ...string) string {
	var builder strings.Builder
	for _, token := range tokens {
		builder.WriteByte('/')
		builder.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(token))
	}
	return builder.String()
}

// checkSharedNames is rule 1.
func checkSharedNames(f *findings, o options) {
	for _, name := range slices.Sorted(maps.Keys(o.Users)) {
		if _, shared := o.Roles[name]; shared {
			f.add(CodeSharedRoleName, "a name may be a login user or a permission role, not both; see the user with the same key", "roles", name)
		}
	}
}

// checkMemberships is rule 2 for users.
func checkMemberships(f *findings, o options) {
	for _, name := range slices.Sorted(maps.Keys(o.Users)) {
		for index, member := range o.Users[name].MemberOf {
			if _, declared := o.Roles[member]; !declared {
				f.add(CodeUnknownMemberRole, "a memberOf entry names no declared role", "users", name, "memberOf", strconv.Itoa(index))
			}
		}
	}
}

// checkDatabase is rules 2, 3 (owner), 4 and 5 for one database.
func checkDatabase(f *findings, o options, name string) {
	db := o.Databases[name]
	owner, ownerKnown := o.Users[db.Owner]
	switch {
	case !ownerKnown:
		f.add(CodeUnknownOwner, "the owner names no declared user", "databases", name, "owner")
	case owner.Purpose != purposeMigrations:
		f.add(CodeOwnerNotMigrationUser, "the owner must be a migration user", "databases", name, "owner")
	}
	connect := map[string]bool{}
	for index, role := range db.ConnectRoles {
		connect[role] = true
		if _, declared := o.Roles[role]; !declared {
			f.add(CodeUnknownConnectRole, "a connectRoles entry names no declared role", "databases", name, "connectRoles", strconv.Itoa(index))
		}
	}
	for _, schemaName := range slices.Sorted(maps.Keys(db.Schemas)) {
		for _, grantee := range slices.Sorted(maps.Keys(db.Schemas[schemaName].Grants)) {
			at := []string{"databases", name, "schemas", schemaName, "grants", grantee}
			switch _, declared := o.Roles[grantee]; {
			case !declared:
				f.add(CodeUnknownGrantee, "a schema grant names no declared role", at...)
			case !connect[grantee]:
				f.add(CodeGranteeWithoutConnect, "a schema grantee must also be among the database's connectRoles", at...)
			}
		}
	}
	for _, settingsUser := range slices.Sorted(maps.Keys(db.UserSettings)) {
		at := []string{"databases", name, "userSettings", settingsUser}
		switch _, declared := o.Users[settingsUser]; {
		case !declared:
			f.add(CodeUnknownSettingsUser, "a userSettings key names no declared user", at...)
		case !hasAccess(o, db, settingsUser):
			f.add(CodeSettingsUserWithoutAccess, "a userSettings key must name the database's owner or a member of one of its connectRoles", at...)
		}
	}
}

// hasAccess reports whether the user owns db or is a member of one of its connectRoles.
func hasAccess(o options, db database, name string) bool {
	if db.Owner == name {
		return true
	}
	return slices.ContainsFunc(o.Users[name].MemberOf, func(role string) bool { return slices.Contains(db.ConnectRoles, role) })
}

// checkOwnership is rule 3 for migration users: only the first database in key order may own one.
func checkOwnership(f *findings, o options) {
	owned := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(o.Databases)) {
		owner := o.Databases[name].Owner
		user, known := o.Users[owner]
		if !known || user.Purpose != purposeMigrations {
			continue
		}
		if owned[owner] {
			f.add(CodeMigrationUserOwnsManyDatabases, "a migration user may own only one database; this is its second or later", "databases", name, "owner")
		}
		owned[owner] = true
	}
}
