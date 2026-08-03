package alipay

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCommonDataTableRow map[string]string

func (r testCommonDataTableRow) ColumnCount() int {
	return len(r)
}

func (r testCommonDataTableRow) HasData(columnName string) bool {
	_, exists := r[columnName]
	return exists
}

func (r testCommonDataTableRow) GetData(columnName string) string {
	return r[columnName]
}

func TestAlipayTransactionDataRowParserPreservesRawRowAndExternalId(t *testing.T) {
	rowParser := &alipayTransactionDataRowParser{
		existedOriginalDataColumns: map[string]bool{
			"交易号":   true,
			"商家订单号": true,
			"交易时间":  true,
			"备注":    true,
		},
	}
	row := testCommonDataTableRow{
		"交易号":   "202601010001",
		"商家订单号": "merchant-001",
		"交易时间":  "2026-01-01 12:00:00",
		"备注":    "coffee",
	}

	externalId := rowParser.getExternalId(row)
	rawPayload, err := rowParser.getRawPayload(row, externalId)
	require.NoError(t, err)

	var payload struct {
		ExternalId string            `json:"externalId"`
		Raw        map[string]string `json:"raw"`
	}
	require.NoError(t, json.Unmarshal([]byte(rawPayload), &payload))

	assert.Equal(t, "trade_no:202601010001", externalId)
	assert.Equal(t, externalId, payload.ExternalId)
	assert.Equal(t, "merchant-001", payload.Raw["商家订单号"])
	assert.Equal(t, "coffee", payload.Raw["备注"])
}
