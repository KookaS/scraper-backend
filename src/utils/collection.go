package utils

import (
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
)

func ImagesCollection(mongoClient *mongo.Client, collection string) (*mongo.Collection, error) {
	switch collection {
	case "wanted":
		return mongoClient.Database(GetEnvVariable("SCRAPER_DB")).Collection(GetEnvVariable("IMAGES_WANTED_COLLECTION")), nil
	case "pending":
		return mongoClient.Database(GetEnvVariable("SCRAPER_DB")).Collection(GetEnvVariable("IMAGES_PENDING_COLLECTION")), nil
	case "unwanted":
		return mongoClient.Database(GetEnvVariable("SCRAPER_DB")).Collection(GetEnvVariable("IMAGES_UNWANTED_COLLECTION")), nil
	default:
		return nil, fmt.Errorf("`%s` does not exist for selecting the images collection. Choose `%s`, `%s` or `%s`",
			collection, "wanted", "pending", "unwanted")
	}
}
