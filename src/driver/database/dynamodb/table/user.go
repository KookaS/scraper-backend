package dynamodb

import (
	"context"
	controllerModel "scraper-backend/src/adapter/controller/model"
	dynamodbModel "scraper-backend/src/driver/database/dynamodb/model"
	"scraper-backend/src/driver/model"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awsDynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type TableUser struct {
	DynamoDbClient *dynamodb.Client
	TableName      string
	PrimaryKey     string // Origin
	SortKey        string // ID
}

func (table TableUser) CreateUser(ctx context.Context, user controllerModel.User) error {
	var driverUser dynamodbModel.User
	driverUser.DriverMarshal(user)

	item, err := attributevalue.MarshalMap(driverUser)
	if err != nil {
		return err
	}

	_, err = table.DynamoDbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(table.TableName),
		Item:      item,
	})
	if err != nil {
		return err
	}

	return nil
}

func (table TableUser) DeleteUser(ctx context.Context, primaryKey string, sortKey model.UUID) error {
	_, err := table.DynamoDbClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(table.TableName),
		Key: map[string]types.AttributeValue{
			table.PrimaryKey: &types.AttributeValueMemberS{
				Value: primaryKey,
			},
			table.SortKey: &types.AttributeValueMemberB{
				Value: sortKey[:],
			},
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func (table TableUser) ReadUser(ctx context.Context, primaryKey string, sortKey model.UUID) (*controllerModel.User, error) {
	input := &awsDynamodb.GetItemInput{
		TableName: aws.String(table.TableName),
		Key: map[string]types.AttributeValue{
			table.PrimaryKey: &types.AttributeValueMemberS{
				Value: primaryKey,
			},
			table.SortKey: &types.AttributeValueMemberB{
				Value: sortKey[:],
			},
		},
	}

	response, err := table.DynamoDbClient.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	var user controllerModel.User
	err = attributevalue.UnmarshalMap(response.Item, &user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (table TableUser) ReadUsers(ctx context.Context, primaryKey string) ([]controllerModel.User, error) {
	var err error
	var response *awsDynamodb.QueryOutput
	var users []dynamodbModel.User
	keyEx := expression.Key(table.PrimaryKey).Equal(expression.Value(primaryKey))
	expr, err := expression.NewBuilder().WithKeyCondition(keyEx).Build()
	if err != nil {
		return nil, err
	}

	response, err = table.DynamoDbClient.Query(ctx, &awsDynamodb.QueryInput{
		TableName:                 aws.String(table.TableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return nil, err
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &users)
	if err != nil {
		return nil, err
	}

	var controllerUsers []controllerModel.User
	for _, user := range users {
		controllerUsers = append(controllerUsers, user.DriverUnmarshal())
	}

	return controllerUsers, nil
}

func (table TableUser) ScanUsers(ctx context.Context) ([]controllerModel.User, error) {
	var err error
	var response *awsDynamodb.ScanOutput
	var users []dynamodbModel.User

	response, err = table.DynamoDbClient.Scan(ctx, &awsDynamodb.ScanInput{
		TableName: aws.String(table.TableName),
	})
	if err != nil {
		return nil, err
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &users)
	if err != nil {
		return nil, err
	}

	var controllerUsers []controllerModel.User
	for _, user := range users {
		controllerUsers = append(controllerUsers, user.DriverUnmarshal())
	}

	return controllerUsers, nil
}