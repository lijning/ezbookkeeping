package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

const importSourceRecordQueryBatchSize = 500
const importCrossSourceTransferMatchWindow = 10 * 60 * 1000

// ImportTransactionsResult contains the persisted import result.
type ImportTransactionsResult struct {
	ImportBatchId        int64
	ImportedRecordCount  int
	DuplicateRecordCount int
}

type importSourceRecordPayload struct {
	Type                 models.TransactionDbType `json:"type"`
	CategoryId           int64                    `json:"categoryId"`
	AccountId            int64                    `json:"accountId"`
	RelatedAccountId     int64                    `json:"relatedAccountId"`
	TransactionTime      int64                    `json:"transactionTime"`
	TimezoneUtcOffset    int16                    `json:"timezoneUtcOffset"`
	Amount               int64                    `json:"amount"`
	RelatedAccountAmount int64                    `json:"relatedAccountAmount"`
	Comment              string                   `json:"comment"`
	GeoLongitude         float64                  `json:"geoLongitude"`
	GeoLatitude          float64                  `json:"geoLatitude"`
}

type importSourceRecordDraft struct {
	transactionIndex int
	record           *models.ImportSourceRecord
}

type matchedImportSourceRecordDraft struct {
	draft         *importSourceRecordDraft
	matchedRecord *models.ImportSourceRecord
	matchGroup    *models.ImportMatchGroup
}

// ImportTransactionService creates immutable source records and their transaction projection.
type ImportTransactionService struct {
	ServiceUsingDB
	ServiceUsingUuid
	transactions *TransactionService
}

// Initialize an import transaction service singleton instance.
var (
	ImportTransactions = &ImportTransactionService{
		ServiceUsingDB: ServiceUsingDB{
			container: datastore.Container,
		},
		ServiceUsingUuid: ServiceUsingUuid{
			container: uuid.Container,
		},
		transactions: Transactions,
	}
)

// ImportTransactions stores source records and their transaction projections atomically.
func (s *ImportTransactionService) ImportTransactions(c core.Context, uid int64, sourceType string, transactions []*models.Transaction, allTagIds map[int][]int64, processHandler core.TaskProcessUpdateHandler) (*ImportTransactionsResult, error) {
	if uid <= 0 {
		return nil, errs.ErrUserIdInvalid
	}

	sourceType = strings.TrimSpace(sourceType)

	if sourceType == "" {
		sourceType = "unknown"
	} else if len(sourceType) > 64 {
		return nil, errs.ErrParameterInvalid
	}

	drafts, contentFingerprint, err := s.buildSourceRecordDrafts(uid, sourceType, transactions)

	if err != nil {
		return nil, err
	}

	existingIdentities, err := s.getExistingSourceIdentities(c, uid, sourceType, drafts)

	if err != nil {
		return nil, err
	}

	newTransactions := make([]*models.Transaction, 0, len(drafts))
	newTransactionTagIds := make(map[int][]int64, len(drafts))
	newTransactionDrafts := make([]*importSourceRecordDraft, 0, len(drafts))
	matchedDrafts := make([]*matchedImportSourceRecordDraft, 0, len(drafts))
	acceptedDrafts := make([]*importSourceRecordDraft, 0, len(drafts))
	seenIdentities := make(map[string]bool, len(drafts))
	duplicateRecordCount := 0

	for _, draft := range drafts {
		sourceIdentity := draft.record.SourceIdentity

		if seenIdentities[sourceIdentity] || existingIdentities[sourceIdentity] {
			duplicateRecordCount++
			continue
		}

		seenIdentities[sourceIdentity] = true
		acceptedDrafts = append(acceptedDrafts, draft)

		matchedRecord, matchErr := s.findCrossSourceTransferMatch(c, uid, draft.record)

		if matchErr != nil {
			return nil, matchErr
		}

		if matchedRecord != nil {
			matchedDrafts = append(matchedDrafts, &matchedImportSourceRecordDraft{
				draft:         draft,
				matchedRecord: matchedRecord,
			})
			continue
		}

		newTransactionIndex := len(newTransactions)
		newTransactions = append(newTransactions, transactions[draft.transactionIndex])
		newTransactionTagIds[newTransactionIndex] = allTagIds[draft.transactionIndex]
		newTransactionDrafts = append(newTransactionDrafts, draft)
	}

	now := time.Now().Unix()
	batch := &models.ImportBatch{
		ImportBatchId:        s.GenerateUuid(uuid.UUID_TYPE_IMPORT_BATCH),
		Uid:                  uid,
		SourceType:           sourceType,
		ContentFingerprint:   contentFingerprint,
		TotalRecordCount:     int32(len(transactions)),
		ImportedRecordCount:  int32(len(acceptedDrafts)),
		DuplicateRecordCount: int32(duplicateRecordCount),
		CreatedUnixTime:      now,
	}

	if batch.ImportBatchId <= 0 {
		return nil, errs.ErrSystemIsBusy
	}

	if len(acceptedDrafts) > 65535 {
		return nil, errs.ErrImportTooManyTransaction
	}

	recordIds := s.GenerateUuids(uuid.UUID_TYPE_IMPORT_SOURCE_RECORD, uint16(len(acceptedDrafts)))

	if len(recordIds) < len(acceptedDrafts) {
		return nil, errs.ErrSystemIsBusy
	}

	for i, draft := range acceptedDrafts {
		draft.record.ImportSourceRecordId = recordIds[i]
		draft.record.ImportBatchId = batch.ImportBatchId
		draft.record.CreatedUnixTime = now
	}

	matchGroupsByMatchedRecordId := make(map[int64]*models.ImportMatchGroup)

	for _, matchedDraft := range matchedDrafts {
		existingMatchGroup, matchGroupErr := s.getMatchGroupBySourceRecordId(c, uid, matchedDraft.matchedRecord.ImportSourceRecordId)

		if matchGroupErr != nil {
			return nil, matchGroupErr
		}

		if existingMatchGroup != nil {
			matchedDraft.matchGroup = existingMatchGroup
			matchedDraft.draft.record.TransactionId = matchedDraft.matchedRecord.TransactionId
			continue
		}

		matchGroup := matchGroupsByMatchedRecordId[matchedDraft.matchedRecord.ImportSourceRecordId]

		if matchGroup == nil {
			matchGroup = &models.ImportMatchGroup{
				MatchGroupId:           s.GenerateUuid(uuid.UUID_TYPE_IMPORT_MATCH_GROUP),
				Uid:                    uid,
				CanonicalTransactionId: matchedDraft.matchedRecord.TransactionId,
				EventType:              models.TRANSACTION_TYPE_TRANSFER,
				Status:                 models.IMPORT_MATCH_GROUP_STATUS_AUTO_CONFIRMED,
				Confidence:             1000,
				RuleVersion:            "transfer-v1",
				CreatedUnixTime:        now,
				UpdatedUnixTime:        now,
			}

			if matchGroup.MatchGroupId <= 0 {
				return nil, errs.ErrSystemIsBusy
			}

			matchGroupsByMatchedRecordId[matchedDraft.matchedRecord.ImportSourceRecordId] = matchGroup
		}

		matchedDraft.matchGroup = matchGroup
		matchedDraft.draft.record.TransactionId = matchedDraft.matchedRecord.TransactionId
	}

	matchGroupMembersCount := len(matchedDrafts) + len(matchGroupsByMatchedRecordId)

	if matchGroupMembersCount > 65535 {
		return nil, errs.ErrImportTooManyTransaction
	}

	matchGroupMemberIds := s.GenerateUuids(uuid.UUID_TYPE_IMPORT_MATCH_MEMBER, uint16(matchGroupMembersCount))

	if len(matchGroupMemberIds) < matchGroupMembersCount {
		return nil, errs.ErrSystemIsBusy
	}

	matchGroupMembers := make([]*models.ImportMatchGroupMember, 0, matchGroupMembersCount)
	matchGroupMemberIndex := 0

	for matchedRecordId, matchGroup := range matchGroupsByMatchedRecordId {
		matchGroupMembers = append(matchGroupMembers, &models.ImportMatchGroupMember{
			MatchGroupMemberId:   matchGroupMemberIds[matchGroupMemberIndex],
			Uid:                  uid,
			MatchGroupId:         matchGroup.MatchGroupId,
			ImportSourceRecordId: matchedRecordId,
			Confidence:           matchGroup.Confidence,
			CreatedUnixTime:      now,
		})
		matchGroupMemberIndex++
	}

	for _, matchedDraft := range matchedDrafts {
		matchGroupMembers = append(matchGroupMembers, &models.ImportMatchGroupMember{
			MatchGroupMemberId:   matchGroupMemberIds[matchGroupMemberIndex],
			Uid:                  uid,
			MatchGroupId:         matchedDraft.matchGroup.MatchGroupId,
			ImportSourceRecordId: matchedDraft.draft.record.ImportSourceRecordId,
			Confidence:           matchedDraft.matchGroup.Confidence,
			CreatedUnixTime:      now,
		})
		matchGroupMemberIndex++
	}

	err = s.transactions.BatchCreateTransactionsWithHandler(c, uid, newTransactions, newTransactionTagIds, processHandler, func(sess *xorm.Session, index int, transaction *models.Transaction) error {
		if index == -1 {
			if _, insertErr := sess.Insert(batch); insertErr != nil {
				return insertErr
			}

			for _, matchGroup := range matchGroupsByMatchedRecordId {
				if _, insertErr := sess.Insert(matchGroup); insertErr != nil {
					return insertErr
				}
			}

			for _, matchedDraft := range matchedDrafts {
				if _, insertErr := sess.Insert(matchedDraft.draft.record); insertErr != nil {
					return insertErr
				}
			}

			for _, matchGroupMember := range matchGroupMembers {
				if _, insertErr := sess.Insert(matchGroupMember); insertErr != nil {
					return insertErr
				}
			}

			return nil
		}

		record := newTransactionDrafts[index].record
		record.TransactionId = transaction.TransactionId
		_, insertErr := sess.Insert(record)
		return insertErr
	})

	if err != nil {
		return nil, err
	}

	return &ImportTransactionsResult{
		ImportBatchId:        batch.ImportBatchId,
		ImportedRecordCount:  len(acceptedDrafts),
		DuplicateRecordCount: duplicateRecordCount,
	}, nil
}

func (s *ImportTransactionService) buildSourceRecordDrafts(uid int64, sourceType string, transactions []*models.Transaction) ([]*importSourceRecordDraft, string, error) {
	drafts := make([]*importSourceRecordDraft, 0, len(transactions))
	contentHasher := sha256.New()
	_, _ = contentHasher.Write([]byte(sourceType))
	_, _ = contentHasher.Write([]byte{0})

	for i, transaction := range transactions {
		payload, err := marshalImportSourceRecordPayload(transaction)

		if err != nil {
			return nil, "", err
		}

		rawPayload := string(payload)
		identityPayload := payload

		if transaction.ImportSourcePayload != "" {
			rawPayload = transaction.ImportSourcePayload
			identityPayload = []byte(transaction.ImportSourcePayload)
		}

		recordFingerprint := hashImportSourceRecord(sourceType, identityPayload)
		externalId := strings.TrimSpace(transaction.ImportExternalId)
		sourceIdentity := "fp:" + recordFingerprint

		if externalId != "" {
			sourceIdentity = "ext:" + hashImportSourceRecord(sourceType, []byte(externalId))
		}
		_, _ = contentHasher.Write([]byte(recordFingerprint))
		_, _ = contentHasher.Write([]byte{0})

		drafts = append(drafts, &importSourceRecordDraft{
			transactionIndex: i,
			record: &models.ImportSourceRecord{
				Uid:               uid,
				SourceType:        sourceType,
				SourceIdentity:    sourceIdentity,
				ExternalId:        externalId,
				RecordFingerprint: recordFingerprint,
				AccountId:         transaction.AccountId,
				RelatedAccountId:  transaction.RelatedAccountId,
				TransactionTime:   transaction.TransactionTime,
				TimezoneUtcOffset: transaction.TimezoneUtcOffset,
				TransactionType:   transaction.Type,
				Amount:            transaction.Amount,
				RelatedAmount:     transaction.RelatedAccountAmount,
				RawPayload:        rawPayload,
			},
		})
	}

	return drafts, hex.EncodeToString(contentHasher.Sum(nil)), nil
}

func (s *ImportTransactionService) getExistingSourceIdentities(c core.Context, uid int64, sourceType string, drafts []*importSourceRecordDraft) (map[string]bool, error) {
	existingIdentities := make(map[string]bool)
	identities := make([]string, 0, len(drafts))
	seenIdentities := make(map[string]bool, len(drafts))

	for _, draft := range drafts {
		sourceIdentity := draft.record.SourceIdentity

		if !seenIdentities[sourceIdentity] {
			seenIdentities[sourceIdentity] = true
			identities = append(identities, sourceIdentity)
		}
	}

	for start := 0; start < len(identities); start += importSourceRecordQueryBatchSize {
		end := start + importSourceRecordQueryBatchSize

		if end > len(identities) {
			end = len(identities)
		}

		var records []*models.ImportSourceRecord
		err := s.UserDataDB(uid).NewSession(c).Cols("source_identity").Where("uid=? AND source_type=?", uid, sourceType).In("source_identity", identities[start:end]).Find(&records)

		if err != nil {
			return nil, err
		}

		for _, record := range records {
			existingIdentities[record.SourceIdentity] = true
		}
	}

	return existingIdentities, nil
}

func (s *ImportTransactionService) getMatchGroupBySourceRecordId(c core.Context, uid int64, sourceRecordId int64) (*models.ImportMatchGroup, error) {
	matchGroupMember := &models.ImportMatchGroupMember{}
	has, err := s.UserDataDB(uid).NewSession(c).
		Where("uid=? AND import_source_record_id=?", uid, sourceRecordId).
		Get(matchGroupMember)

	if err != nil {
		return nil, err
	} else if !has {
		return nil, nil
	}

	matchGroup := &models.ImportMatchGroup{}
	has, err = s.UserDataDB(uid).NewSession(c).
		ID(matchGroupMember.MatchGroupId).
		Where("uid=?", uid).
		Get(matchGroup)

	if err != nil {
		return nil, err
	} else if !has {
		return nil, errs.ErrDatabaseOperationFailed
	}

	return matchGroup, nil
}

func (s *ImportTransactionService) findCrossSourceTransferMatch(c core.Context, uid int64, record *models.ImportSourceRecord) (*models.ImportSourceRecord, error) {
	if record.TransactionType != models.TRANSACTION_DB_TYPE_TRANSFER_OUT ||
		record.AccountId <= 0 ||
		record.RelatedAccountId <= 0 ||
		record.Amount < 0 ||
		record.RelatedAmount < 0 {
		return nil, nil
	}

	minTransactionTime := record.TransactionTime - importCrossSourceTransferMatchWindow
	maxTransactionTime := record.TransactionTime + importCrossSourceTransferMatchWindow
	var candidates []*models.ImportSourceRecord
	err := s.UserDataDB(uid).NewSession(c).
		Where("uid=? AND transaction_type=? AND account_id=? AND related_account_id=? AND amount=? AND related_amount=? AND transaction_time>=? AND transaction_time<=?",
			uid,
			models.TRANSACTION_DB_TYPE_TRANSFER_OUT,
			record.RelatedAccountId,
			record.AccountId,
			record.RelatedAmount,
			record.Amount,
			minTransactionTime,
			maxTransactionTime,
		).
		Limit(2).
		Find(&candidates)

	if err != nil {
		return nil, err
	}

	if len(candidates) != 1 {
		return nil, nil
	}

	if !canAutoMatchCrossSourceTransfer(record, candidates[0]) {
		return nil, nil
	}

	active, err := s.UserDataDB(uid).NewSession(c).
		Where("uid=? AND deleted=? AND transaction_id=?", uid, false, candidates[0].TransactionId).
		Exist(&models.Transaction{})

	if err != nil {
		return nil, err
	} else if !active {
		return nil, nil
	}

	return candidates[0], nil
}

func canAutoMatchCrossSourceTransfer(record *models.ImportSourceRecord, candidate *models.ImportSourceRecord) bool {
	if record == nil ||
		candidate == nil ||
		record.SourceIdentity == candidate.SourceIdentity ||
		record.TransactionType != models.TRANSACTION_DB_TYPE_TRANSFER_OUT ||
		candidate.TransactionType != models.TRANSACTION_DB_TYPE_TRANSFER_OUT ||
		record.AccountId != candidate.RelatedAccountId ||
		record.RelatedAccountId != candidate.AccountId ||
		record.Amount != candidate.RelatedAmount ||
		record.RelatedAmount != candidate.Amount {
		return false
	}

	timeDifference := record.TransactionTime - candidate.TransactionTime

	if timeDifference < 0 {
		timeDifference = -timeDifference
	}

	return timeDifference <= importCrossSourceTransferMatchWindow
}

func marshalImportSourceRecordPayload(transaction *models.Transaction) ([]byte, error) {
	return json.Marshal(&importSourceRecordPayload{
		Type:                 transaction.Type,
		CategoryId:           transaction.CategoryId,
		AccountId:            transaction.AccountId,
		RelatedAccountId:     transaction.RelatedAccountId,
		TransactionTime:      transaction.TransactionTime,
		TimezoneUtcOffset:    transaction.TimezoneUtcOffset,
		Amount:               transaction.Amount,
		RelatedAccountAmount: transaction.RelatedAccountAmount,
		Comment:              transaction.Comment,
		GeoLongitude:         transaction.GeoLongitude,
		GeoLatitude:          transaction.GeoLatitude,
	})
}

func hashImportSourceRecord(sourceType string, payload []byte) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(sourceType))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(payload)
	return hex.EncodeToString(hasher.Sum(nil))
}
