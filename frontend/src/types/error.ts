export enum GeneralErrorCode {
  OK = 0,
  INTERNAL = 1,
  NOT_AUTHORIZED = 2,
  INVALID = 3,
  NOT_FOUND = 4,
  CONFLICT = 5,
  NOT_IMPLEMENTED = 6,
}

export enum DBErrorCode {
  CONNECTION_ERROR = 101,
  SYNTAX_ERROR = 102,
  EXECUTION_ERROR = 103,
}

export enum MigrationErrorCode {
  MIGRATION_SCHEMA_MISSING = 201,
  MIGRATION_ALREADY_APPLIED = 202,
  MIGRATION_OUT_OF_ORDER = 203,
  MIGRATION_BASELINE_MISSING = 204,
}

export enum SQLReviewPolicyErrorCode {
  EMPTY_POLICY = 2,
  STATEMENT_NO_WHERE = 202,
  STATEMENT_NO_SELECT_ALL = 203,
  STATEMENT_LEADING_WILDCARD_LIKE = 204,
  TABLE_NAMING_MISMATCH = 301,
  COLUMN_NAMING_MISMATCH = 302,
  INDEX_NAMING_MISMATCH = 303,
  UK_NAMING_MISMATCH = 304,
  FK_NAMING_MISMATCH = 305,
  PK_NAMING_MISMATCH = 306,
  NO_REQUIRED_COLUMN = 401,
  COLUMN_CANBE_NULL = 402,
  NOT_INNODB_ENGINE = 501,
  NO_PK_IN_TABLE = 601,
  FK_IN_TABLE = 602,
  TABLE_DROP_NAMING_CONVENTION = 603,
  DATABASE_NOT_EMPTY = 701,
}

export enum CompatibilityErrorCode {
  COMPATIBILITY_DROP_DATABASE = 101,
  COMPATIBILITY_RENAME_TABLE = 102,
  COMPATIBILITY_DROP_TABLE = 103,
  COMPATIBILITY_RENAME_COLUMN = 104,
  COMPATIBILITY_DROP_COLUMN = 105,
  COMPATIBILITY_ADD_PRIMARY_KEY = 106,
  COMPATIBILITY_ADD_UNIQUE_KEY = 107,
  COMPATIBILITY_ADD_FOREIGN_KEY = 108,
  COMPATIBILITY_ADD_CHECK = 109,
  COMPATIBILITY_ALTER_CHECK = 110,
  COMPATIBILITY_ALTER_COLUMN = 111,
}

export type ErrorCode =
  | GeneralErrorCode
  | DBErrorCode
  | MigrationErrorCode
  | CompatibilityErrorCode
  | SQLReviewPolicyErrorCode;

export type ErrorTag = "General" | "Compatibility";

export type ErrorMeta = {
  code: ErrorCode;
  hash: string;
};
