package models

// ImportBatch represents one transaction import attempt.
type ImportBatch struct {
	ImportBatchId        int64  `xorm:"PK"`
	Uid                  int64  `xorm:"INDEX(IDX_import_batch_uid_created_time) NOT NULL"`
	SourceType           string `xorm:"VARCHAR(64) INDEX(IDX_import_batch_uid_source_type_created_time) NOT NULL"`
	ContentFingerprint   string `xorm:"VARCHAR(64) INDEX(IDX_import_batch_uid_source_type_created_time) NOT NULL"`
	TotalRecordCount     int32  `xorm:"NOT NULL"`
	ImportedRecordCount  int32  `xorm:"NOT NULL"`
	DuplicateRecordCount int32  `xorm:"NOT NULL"`
	CreatedUnixTime      int64  `xorm:"INDEX(IDX_import_batch_uid_created_time) NOT NULL"`
}

// ImportMatchGroupStatus represents the confirmation state of a cross-source match.
type ImportMatchGroupStatus byte

// Import match group statuses.
const (
	IMPORT_MATCH_GROUP_STATUS_AUTO_CONFIRMED   ImportMatchGroupStatus = 1
	IMPORT_MATCH_GROUP_STATUS_MANUAL_CONFIRMED ImportMatchGroupStatus = 2
)

// ImportMatchGroup represents multiple source records that observe one transaction projection.
type ImportMatchGroup struct {
	MatchGroupId int64 `xorm:"PK"`
	Uid          int64 `xorm:"INDEX(IDX_import_match_group_uid_transaction_id) NOT NULL"`
	// CanonicalTransactionId currently references the compatible transaction projection.
	CanonicalTransactionId int64                  `xorm:"INDEX(IDX_import_match_group_uid_transaction_id) NOT NULL"`
	EventType              TransactionType        `xorm:"NOT NULL"`
	Status                 ImportMatchGroupStatus `xorm:"NOT NULL"`
	Confidence             int16                  `xorm:"NOT NULL"`
	RuleVersion            string                 `xorm:"VARCHAR(32) NOT NULL"`
	CreatedUnixTime        int64                  `xorm:"NOT NULL"`
	UpdatedUnixTime        int64                  `xorm:"NOT NULL"`
}

// ImportMatchGroupMember relates one immutable source record to one match group.
type ImportMatchGroupMember struct {
	MatchGroupMemberId   int64 `xorm:"PK"`
	Uid                  int64 `xorm:"UNIQUE(UQE_import_match_group_member_uid_source_record_id) INDEX(IDX_import_match_group_member_uid_match_group_id) NOT NULL"`
	MatchGroupId         int64 `xorm:"INDEX(IDX_import_match_group_member_uid_match_group_id) NOT NULL"`
	ImportSourceRecordId int64 `xorm:"UNIQUE(UQE_import_match_group_member_uid_source_record_id) NOT NULL"`
	Confidence           int16 `xorm:"NOT NULL"`
	CreatedUnixTime      int64 `xorm:"NOT NULL"`
}

// ImportSourceRecord represents one immutable observation from an imported source.
type ImportSourceRecord struct {
	ImportSourceRecordId int64             `xorm:"PK"`
	Uid                  int64             `xorm:"UNIQUE(UQE_import_source_record_uid_source_identity) INDEX(IDX_import_source_record_uid_batch_id) INDEX(IDX_import_source_record_uid_transaction_id) NOT NULL"`
	ImportBatchId        int64             `xorm:"INDEX(IDX_import_source_record_uid_batch_id) NOT NULL"`
	SourceType           string            `xorm:"VARCHAR(64) UNIQUE(UQE_import_source_record_uid_source_identity) NOT NULL"`
	SourceIdentity       string            `xorm:"VARCHAR(128) UNIQUE(UQE_import_source_record_uid_source_identity) NOT NULL"`
	ExternalId           string            `xorm:"VARCHAR(255) INDEX(IDX_import_source_record_uid_external_id) NOT NULL"`
	RecordFingerprint    string            `xorm:"VARCHAR(64) NOT NULL"`
	TransactionId        int64             `xorm:"INDEX(IDX_import_source_record_uid_transaction_id) NOT NULL"`
	AccountId            int64             `xorm:"NOT NULL"`
	RelatedAccountId     int64             `xorm:"NOT NULL"`
	TransactionTime      int64             `xorm:"NOT NULL"`
	TimezoneUtcOffset    int16             `xorm:"NOT NULL"`
	TransactionType      TransactionDbType `xorm:"NOT NULL"`
	Amount               int64             `xorm:"NOT NULL"`
	RelatedAmount        int64             `xorm:"NOT NULL"`
	RawPayload           string            `xorm:"TEXT NOT NULL"`
	CreatedUnixTime      int64             `xorm:"NOT NULL"`
}
