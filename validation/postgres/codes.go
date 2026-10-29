package postgres

import "github.com/TomazVidovic533/stackpuddle/model"

// The stable codes of the postgres rules, "postgres.<rule>". Each is a published name: it is kept or
// retired explicitly, never renamed. Messages are for people and are not part of the contract.
const (
	// CodeSharedRoleName reports a name declared both under users and under roles. Its pointer is
	// the role.
	CodeSharedRoleName model.Code = "postgres.shared_role_name"
	// CodeUnknownMemberRole reports a memberOf entry that is not a declared role.
	CodeUnknownMemberRole model.Code = "postgres.unknown_member_role"
	// CodeUnknownOwner reports a database owner that is not a declared user.
	CodeUnknownOwner model.Code = "postgres.unknown_owner"
	// CodeUnknownConnectRole reports a connectRoles entry that is not a declared role.
	CodeUnknownConnectRole model.Code = "postgres.unknown_connect_role"
	// CodeUnknownGrantee reports a schema grant to a name that is not a declared role.
	CodeUnknownGrantee model.Code = "postgres.unknown_grantee"
	// CodeUnknownSettingsUser reports a userSettings key that is not a declared user.
	CodeUnknownSettingsUser model.Code = "postgres.unknown_settings_user"
	// CodeOwnerNotMigrationUser reports a database owner whose purpose is not migrations.
	CodeOwnerNotMigrationUser model.Code = "postgres.owner_not_migration_user"
	// CodeMigrationUserOwnsManyDatabases reports a migration user that owns a second or later
	// database. Its pointer is that database's owner.
	CodeMigrationUserOwnsManyDatabases model.Code = "postgres.migration_user_owns_multiple_databases"
	// CodeGranteeWithoutConnect reports a schema grantee that is not among its database's
	// connectRoles.
	CodeGranteeWithoutConnect model.Code = "postgres.grantee_without_connect"
	// CodeSettingsUserWithoutAccess reports a userSettings key for a user that has no access to
	// the database.
	CodeSettingsUserWithoutAccess model.Code = "postgres.settings_user_without_access"
)
