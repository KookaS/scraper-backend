package gin

import (
	"context"
	dynamodbTable "scraper-backend/src/driver/database/dynamodb/table"
	serverModel "scraper-backend/src/driver/server/model"

	"github.com/google/uuid"
)

func (d DriverServerGin) CreateTag(ctx context.Context, tag serverModel.Tag) (string, error) {
	tag.Type = dynamodbTable.TagPrimaryKeySearched
	err := d.ControllerTag.CreateTag(ctx, tag.DriverUnmarshal())
	if err != nil {
		return "error", err
	}
	return "ok", nil
}

func (d DriverServerGin) CreateTagBlocked(ctx context.Context, tag serverModel.Tag) (string, error) {
	tag.Type = dynamodbTable.TagPrimaryKeyBlocked
	err := d.ControllerTag.CreateTag(ctx, tag.DriverUnmarshal())
	if err != nil {
		return "error", err
	}
	return "ok", nil
}

type ParamsDeleteTag struct {
	ID string `uri:"id" binding:"required"`
}

func (d DriverServerGin) DeleteTag(ctx context.Context, params ParamsDeleteTag) (string, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return "error", err
	}
	if err := d.ControllerTag.DeleteTag(ctx, dynamodbTable.TagPrimaryKeySearched, id); err != nil {
		return "error", err
	}
	return "ok", nil
}

func (d DriverServerGin) DeleteTagBlocked(ctx context.Context, params ParamsDeleteTag) (string, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return "error", err
	}
	if err := d.ControllerTag.DeleteTag(ctx, dynamodbTable.TagPrimaryKeyBlocked, id); err != nil {
		return "error", err
	}
	return "ok", nil
}

func (d DriverServerGin) ReadTags(ctx context.Context) ([]serverModel.Tag, error) {
	controllerTags, err := d.ControllerTag.ReadTags(ctx, dynamodbTable.TagPrimaryKeySearched)
	if err != nil {
		return nil, err
	}
	serverTags := make([]serverModel.Tag, len(controllerTags))
	for i, controllerTag := range controllerTags {
		serverTags[i].DriverMarshal(controllerTag)
	}
	return serverTags, nil
}

func (d DriverServerGin) ReadTagsBlocked(ctx context.Context) ([]serverModel.Tag, error) {
	controllerTags, err := d.ControllerTag.ReadTags(ctx, dynamodbTable.TagPrimaryKeyBlocked)
	if err != nil {
		return nil, err
	}
	serverTags := make([]serverModel.Tag, len(controllerTags))
	for i, controllerTag := range controllerTags {
		serverTags[i].DriverMarshal(controllerTag)
	}
	return serverTags, nil
}
