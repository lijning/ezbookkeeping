package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/models"
)

func TestBuildSourceRecordDrafts_StableFingerprint(t *testing.T) {
	service := &ImportTransactionService{}
	transactions := []*models.Transaction{
		{
			Type:                 models.TRANSACTION_DB_TYPE_EXPENSE,
			CategoryId:           100,
			AccountId:            200,
			TransactionTime:      1743500000000,
			TimezoneUtcOffset:    480,
			Amount:               12345,
			RelatedAccountAmount: 0,
			Comment:              "Coffee",
		},
	}

	drafts, contentFingerprint, err := service.buildSourceRecordDrafts(1, "alipay:web", transactions)

	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Len(t, contentFingerprint, 64)
	assert.Equal(t, "fp:"+drafts[0].record.RecordFingerprint, drafts[0].record.SourceIdentity)
	assert.Equal(t, int64(1), drafts[0].record.Uid)
	assert.Equal(t, int64(200), drafts[0].record.AccountId)

	repeatedDrafts, repeatedContentFingerprint, err := service.buildSourceRecordDrafts(1, "alipay:web", transactions)

	require.NoError(t, err)
	assert.Equal(t, drafts[0].record.RecordFingerprint, repeatedDrafts[0].record.RecordFingerprint)
	assert.Equal(t, contentFingerprint, repeatedContentFingerprint)
}

func TestBuildSourceRecordDrafts_DifferentSourcesDoNotShareIdentity(t *testing.T) {
	service := &ImportTransactionService{}
	transactions := []*models.Transaction{
		{
			Type:              models.TRANSACTION_DB_TYPE_EXPENSE,
			AccountId:         200,
			TransactionTime:   1743500000000,
			TimezoneUtcOffset: 480,
			Amount:            12345,
			Comment:           "Coffee",
		},
	}

	alipayDrafts, _, err := service.buildSourceRecordDrafts(1, "alipay:web", transactions)
	require.NoError(t, err)

	wechatDrafts, _, err := service.buildSourceRecordDrafts(1, "wechat:csv", transactions)
	require.NoError(t, err)

	assert.NotEqual(t, alipayDrafts[0].record.SourceIdentity, wechatDrafts[0].record.SourceIdentity)
}

func TestBuildSourceRecordDrafts_UsesImportSourcePayloadAsFingerprint(t *testing.T) {
	service := &ImportTransactionService{}
	transaction := &models.Transaction{
		Type:                models.TRANSACTION_DB_TYPE_EXPENSE,
		AccountId:           200,
		TransactionTime:     1743500000000,
		TimezoneUtcOffset:   480,
		Amount:              12345,
		ImportSourcePayload: `{"tradeNo":"202601010001","counterparty":"Coffee Shop"}`,
	}

	drafts, _, err := service.buildSourceRecordDrafts(1, "alipay:web", []*models.Transaction{transaction})
	require.NoError(t, err)

	transaction.CategoryId = 900
	transaction.AccountId = 300
	reimportedDrafts, _, err := service.buildSourceRecordDrafts(1, "alipay:web", []*models.Transaction{transaction})
	require.NoError(t, err)

	assert.Equal(t, transaction.ImportSourcePayload, drafts[0].record.RawPayload)
	assert.Equal(t, drafts[0].record.RecordFingerprint, reimportedDrafts[0].record.RecordFingerprint)
}

func TestBuildSourceRecordDrafts_UsesExternalIdAsSourceIdentity(t *testing.T) {
	service := &ImportTransactionService{}
	transaction := &models.Transaction{
		Type:                models.TRANSACTION_DB_TYPE_EXPENSE,
		AccountId:           200,
		TransactionTime:     1743500000000,
		TimezoneUtcOffset:   480,
		Amount:              12345,
		ImportSourcePayload: `{"raw":{"交易号":"202601010001"}}`,
		ImportExternalId:    "trade_no:202601010001",
	}

	drafts, _, err := service.buildSourceRecordDrafts(1, "alipay:web", []*models.Transaction{transaction})
	require.NoError(t, err)

	transaction.ImportSourcePayload = `{"raw":{"交易号":"202601010001","备注":"updated"}}`
	reimportedDrafts, _, err := service.buildSourceRecordDrafts(1, "alipay:web", []*models.Transaction{transaction})
	require.NoError(t, err)

	assert.Equal(t, "trade_no:202601010001", drafts[0].record.ExternalId)
	assert.Contains(t, drafts[0].record.SourceIdentity, "ext:")
	assert.Equal(t, drafts[0].record.SourceIdentity, reimportedDrafts[0].record.SourceIdentity)
	assert.NotEqual(t, drafts[0].record.RecordFingerprint, reimportedDrafts[0].record.RecordFingerprint)
}

func TestCanAutoMatchCrossSourceTransfer(t *testing.T) {
	transferOut := &models.ImportSourceRecord{
		SourceIdentity:   "fp:source-a",
		TransactionType:  models.TRANSACTION_DB_TYPE_TRANSFER_OUT,
		AccountId:        100,
		RelatedAccountId: 200,
		TransactionTime:  1743500000000,
		Amount:           10000,
		RelatedAmount:    10000,
	}
	transferInObservation := &models.ImportSourceRecord{
		SourceIdentity:   "fp:source-b",
		TransactionType:  models.TRANSACTION_DB_TYPE_TRANSFER_OUT,
		AccountId:        200,
		RelatedAccountId: 100,
		TransactionTime:  1743500300000,
		Amount:           10000,
		RelatedAmount:    10000,
	}

	assert.True(t, canAutoMatchCrossSourceTransfer(transferOut, transferInObservation))

	transferInObservation.RelatedAccountId = 300
	assert.False(t, canAutoMatchCrossSourceTransfer(transferOut, transferInObservation))

	transferInObservation.RelatedAccountId = 100
	transferInObservation.TransactionTime += importCrossSourceTransferMatchWindow + 1
	assert.False(t, canAutoMatchCrossSourceTransfer(transferOut, transferInObservation))
}

func TestCanAutoMatchCrossSourceTransfer_DoesNotMatchExpense(t *testing.T) {
	expense := &models.ImportSourceRecord{
		SourceIdentity:  "fp:expense-a",
		TransactionType: models.TRANSACTION_DB_TYPE_EXPENSE,
		AccountId:       100,
		TransactionTime: 1743500000000,
		Amount:          10000,
	}
	income := &models.ImportSourceRecord{
		SourceIdentity:  "fp:income-b",
		TransactionType: models.TRANSACTION_DB_TYPE_INCOME,
		AccountId:       200,
		TransactionTime: 1743500300000,
		Amount:          10000,
	}

	assert.False(t, canAutoMatchCrossSourceTransfer(expense, income))
}
