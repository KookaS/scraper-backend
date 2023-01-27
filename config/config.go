package config

import (
	"fmt"
	"os"

	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

type ConfigDynamodb struct {
	Databases map[string]ConfigDynamodbTable `mapstructure:"dynamodb"`
}

type ConfigDynamodbTable struct {
	Name           *string `mapstructure:"name"`
	PrimaryKeyName *string `mapstructure:"primaryKeyName"`
	PrimaryKeyType *string `mapstructure:"primaryKeyType"`
	SortKeyName    *string `mapstructure:"sortKeyName"`
	SortKeyType    *string `mapstructure:"sortKeyType"`
}

func ReadConfigFile(path string) (*ConfigDynamodb, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var c ConfigDynamodb
	var raw interface{}

	if err := yaml.Unmarshal(f, &raw); err != nil {
		return nil, err
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{WeaklyTypedInput: true, Result: &c})
	if err != nil {
		return nil, err
	}

	if err := decoder.Decode(raw); err != nil {
		return nil, err
	}

	if len(c.Databases) == 0 {
		return nil, fmt.Errorf("no database found")
	}

	for tableReference, tableConfig := range c.Databases {
		if tableConfig.Name == nil || tableConfig.PrimaryKeyName == nil || tableConfig.PrimaryKeyType == nil || tableConfig.SortKeyName == nil || tableConfig.SortKeyType == nil {
			return nil, fmt.Errorf("element missing for table %+#v", tableReference)
		}
	}

	return &c, nil
}
