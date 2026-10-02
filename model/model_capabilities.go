package model

import (
	"database/sql/driver"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type ModelCapabilities dto.ModelCapabilities

func (card ModelCapabilities) Validate() error { return dto.ModelCapabilities(card).Validate() }

// Value uses TEXT rather than dialect-specific JSON columns in all databases.
func (card ModelCapabilities) Value() (driver.Value, error) {
	if err := card.Validate(); err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(card)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func (card *ModelCapabilities) Scan(value any) error {
	var encoded []byte
	switch value := value.(type) {
	case nil:
		*card = ModelCapabilities{}
		return nil
	case string:
		encoded = []byte(value)
	case []byte:
		encoded = value
	default:
		return fmt.Errorf("unsupported model capabilities database value %T", value)
	}
	var decoded ModelCapabilities
	if err := common.Unmarshal(encoded, &decoded); err != nil {
		return err
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*card = decoded
	return nil
}
