package controller

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"path/filepath"
	controllerModel "scraper-backend/src/adapter/controller/model"
	databaseInterface "scraper-backend/src/adapter/interface/database"
	storageInterface "scraper-backend/src/adapter/interface/storage"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/google/uuid"
)

type ControllerPicture struct {
	S3                 storageInterface.DriverS3
	BucketName         string
	DynamodbProcess    databaseInterface.DriverDynamodbPicture
	DynamodbValidation databaseInterface.DriverDynamodbPicture
	DynamodbProduction databaseInterface.DriverDynamodbPicture
	DynamodbBlocked    databaseInterface.DriverDynamodbPicture
	PrimaryKey         string
	SortKey            string
}

func (c ControllerPicture) driverDynamodbMap(state string) (databaseInterface.DriverDynamodbPicture, error) {
	switch state {
	case "production":
		return c.DynamodbProduction, nil
	case "validation":
		return c.DynamodbValidation, nil
	case "process":
		return c.DynamodbProcess, nil
	default:
		return nil, fmt.Errorf("table name %s not available", state)
	}
}

func (c ControllerPicture) CreatePicture(ctx context.Context, picture controllerModel.Picture) error {
	return c.DynamodbProcess.CreatePicture(ctx, picture)
}

func (c ControllerPicture) DeletePicture(ctx context.Context, picture controllerModel.Picture) error {
	return c.DynamodbProcess.DeletePicture(ctx, picture.Origin, picture.Name)
}

func (c ControllerPicture) DeletePictureAndFile(ctx context.Context, picture controllerModel.Picture) error {
	if err := c.DynamodbProcess.DeletePicture(ctx, picture.Origin, picture.Name); err != nil {
		return err
	}
	path := filepath.Join(picture.Origin, picture.Name)
	return c.S3.ItemDelete(ctx, c.BucketName, path)
}

func (c ControllerPicture) DeletePicturesAndFiles(ctx context.Context, pictures []controllerModel.Picture) error {
	for _, picture := range pictures {
		if err := c.DynamodbProcess.DeletePicture(ctx, picture.Origin, picture.Name); err != nil {
			return err
		}
		path := filepath.Join(picture.Origin, picture.Name)
		if err := c.S3.ItemDelete(ctx, c.BucketName, path); err != nil {
			return err
		}
	}
	return nil
}

func (c ControllerPicture) CreatePictureTag(ctx context.Context, picture controllerModel.Picture, tag controllerModel.PictureTag) error {
	if err := c.DynamodbProcess.CreatePictureTag(ctx, picture.Origin, picture.Name, tag); err != nil {
		return err
	}
	return nil
}

func (c ControllerPicture) UpdatePictureTag(ctx context.Context, picture controllerModel.Picture, tagID uuid.UUID, tag controllerModel.PictureTag) error {
	if err := c.DynamodbProcess.UpdatePictureTag(ctx, picture.Origin, picture.Name, tagID, tag); err != nil {
		return err
	}
	return nil
}

func (c ControllerPicture) DeletePictureTag(ctx context.Context, picture controllerModel.Picture, tagID uuid.UUID) error {
	if err := c.DynamodbProcess.DeletePictureTag(ctx, picture.Origin, picture.Name, tagID); err != nil {
		return err
	}
	return nil
}

func (c ControllerPicture) UpdatePictureSize(ctx context.Context, box controllerModel.Box, picture controllerModel.Picture, imageSizeID uuid.UUID) error {
	newPicture, err := c.cropPicture(ctx, box, picture, imageSizeID)
	if err != nil {
		return err
	}

	newFile, err := c.cropFile(ctx, box, picture)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s/%s.%s", newPicture.Origin, newPicture.Name, newPicture.Extension)
	buffer, err := fileToBuffer(*newPicture, newFile)
	if err != nil {
		return err
	}
	if err := c.S3.ItemCreate(ctx, buffer, c.BucketName, path); err != nil {
		return err
	}

	if err := c.DynamodbProcess.CreatePicture(ctx, *newPicture); err != nil {
		return err
	}
	return nil
}

func (c ControllerPicture) CopyPicture(ctx context.Context, picture controllerModel.Picture) error {
	newPicture, err := c.DynamodbProcess.ReadPicture(ctx, picture.Origin, picture.Name)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s_%s", newPicture.OriginID, time.Now().Format(time.RFC3339))
	newPicture.Name = name
	newPicture.CreationDate = time.Now()

	sourcePath := fmt.Sprintf("%s/%s.%s", newPicture.Origin, newPicture.Name, newPicture.Extension)
	destinationPath := fmt.Sprintf("%s/%s.%s", newPicture.Origin, name, newPicture.Extension)
	if err := c.S3.ItemCopy(ctx, c.BucketName, sourcePath, destinationPath); err != nil {
		return err
	}

	if err := c.DynamodbProcess.CreatePicture(ctx, *newPicture); err != nil {
		return err
	}
	return nil
}

func (c ControllerPicture) TransferPicture(ctx context.Context, picture controllerModel.Picture, from, to string) error {
	fromDynamodb, err := c.driverDynamodbMap(from)
	if err != nil {
		return err
	}

	oldPicture, err := fromDynamodb.ReadPicture(ctx, picture.Origin, picture.Name)
	if err != nil {
		return err
	}

	toDynamodb, err := c.driverDynamodbMap(to)
	if err != nil {
		return err
	}

	if err := toDynamodb.CreatePicture(ctx, *oldPicture); err != nil {
		return err
	}

	if err := fromDynamodb.DeletePicture(ctx, picture.Origin, picture.Name); err != nil {
		return err
	}

	return nil
}

func (c ControllerPicture) CreatePictureBlocked(ctx context.Context, picture controllerModel.Picture) error {
	sourcePath := fmt.Sprintf("%s/%s.%s", picture.Origin, picture.Name, picture.Extension)
	if err := c.S3.ItemDelete(ctx, c.BucketName, sourcePath); err != nil {
		return err
	}

	picture.CreationDate = time.Now()
	if err := c.DynamodbBlocked.CreatePicture(ctx, picture); err != nil {
		return err
	}

	if err := c.DynamodbProcess.DeletePicture(ctx, picture.Origin, picture.Name); err != nil {
		return err
	}

	return nil
}

func (c ControllerPicture) DeletePictureBlocked(ctx context.Context, picture controllerModel.Picture) error {
	return c.DynamodbBlocked.DeletePicture(ctx, picture.Origin, picture.Name)
}

func fileToBuffer(picture controllerModel.Picture, file image.Image) (*bytes.Buffer, error) {
	// create buffer
	buffer := new(bytes.Buffer)
	// encode image to buffer

	if picture.Extension == "jpeg" || picture.Extension == "jpg" {
		err := jpeg.Encode(buffer, file, nil)
		if err != nil {
			return nil, fmt.Errorf("jpeg.Encode has failed: %v", err)
		}
	} else if picture.Extension == "png" {
		err := png.Encode(buffer, file)
		if err != nil {
			return nil, fmt.Errorf("png.Encode has failed: %v", err)
		}
	} else {
		return nil, fmt.Errorf("no image extension matching the buffer conversion")
	}
	return buffer, nil
}

func (c ControllerPicture) cropPicture(ctx context.Context, box controllerModel.Box, picture controllerModel.Picture, imageSizeID uuid.UUID) (*controllerModel.Picture, error) {
	oldPicture, err := c.DynamodbProcess.ReadPicture(ctx, picture.Origin, picture.Name)
	if err != nil {
		return nil, err
	}
	return updatePictureTagBoxes(box, *oldPicture, imageSizeID)
}

func updatePictureTagBoxes(box controllerModel.Box, picture controllerModel.Picture, imageSizeID uuid.UUID) (*controllerModel.Picture, error) {
	// new size creation
	size := controllerModel.PictureSize{
		CreationDate: time.Now(),
		Box:          box, // absolute position
	}
	picture.Sizes[uuid.New()] = size

	for tagID, tag := range picture.Tags {
		if tag.BoxInformation.Valid {
			boxInformation := tag.BoxInformation.Body
			// relative position of tags
			tlx := boxInformation.Box.Tlx
			tly := boxInformation.Box.Tly
			width := boxInformation.Box.Width
			height := boxInformation.Box.Height

			// box outside on the image right
			if tlx > box.Tlx+box.Width {
				delete(picture.Tags, tagID)
				continue
			}
			// box left outside on the image left
			if tlx < box.Tlx {
				// box outside on the image left
				if tlx+width < box.Tlx {
					width = 0
				} else { // box right inside the image
					width = width - box.Tlx + tlx
				}
				tlx = box.Tlx
			} else { // box left inside image
				// box right outside on the image right
				if tlx+width > box.Tlx+box.Width {
					width = box.Tlx + box.Width - tlx
				}
				tlx = tlx - box.Tlx
			}
			// box width too small
			if width < 50 {
				delete(picture.Tags, tagID)
				continue
			}

			// box outside at the image bottom
			if tly > box.Tly+box.Height {
				delete(picture.Tags, tagID)
				continue
			}
			// box top outside on the image top
			if tly < box.Tly {
				// box outside on the image top
				if tly+height < box.Tly {
					height = 0
				} else { // box bottom inside the image
					height = height - box.Tly + tly
				}
				tly = box.Tly
			} else { // box top inside image
				// box bottom outside on the image bottom
				if tly+height > box.Tly+box.Height {
					height = box.Tly + box.Height - tly
				}
				tly = tly - box.Tly
			}
			// box height too small
			if height < 50 {
				delete(picture.Tags, tagID)
				continue
			}

			// set the new relative reference to the newly cropped image
			tag.BoxInformation.Body.ImageSizeID = imageSizeID
			tag.BoxInformation.Body.Box.Tlx = tlx
			tag.BoxInformation.Body.Box.Tly = tly
			tag.BoxInformation.Body.Box.Width = width
			tag.BoxInformation.Body.Box.Height = height
		}
		picture.Tags[tagID] = tag
	}
	return &picture, nil
}

func (c ControllerPicture) cropFile(ctx context.Context, box controllerModel.Box, picture controllerModel.Picture) (image.Image, error) {
	path := filepath.Join(picture.Origin, picture.Name)
	buffer, err := c.S3.ItemRead(ctx, c.BucketName, path)
	if err != nil {
		return nil, err
	}

	// convert []byte to image
	img, _, _ := image.Decode(bytes.NewReader(buffer))

	// crop the image with the bounding box rectangle
	cropRect := image.Rect(box.Tlx, box.Tly, box.Tlx+box.Width, box.Tly+box.Height)
	img, err = updateFileDimension(img, cropRect)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func updateFileDimension(img image.Image, cropRect image.Rectangle) (image.Image, error) {
	//Interface for asserting whether `img`
	//implements SubImage or not.
	//This can be defined globally.
	type CropableImage interface {
		image.Image
		SubImage(r image.Rectangle) image.Image
	}

	if p, ok := img.(CropableImage); ok {
		// Call SubImage. This should be fast,
		// since SubImage (usually) shares underlying pixel.
		return p.SubImage(cropRect), nil
	} else if cropRect = cropRect.Intersect(img.Bounds()); !cropRect.Empty() {
		// If `img` does not implement `SubImage`,
		// copy (and silently convert) the image portion to RGBA image.
		rgbaImg := image.NewRGBA(cropRect)
		for y := cropRect.Min.Y; y < cropRect.Max.Y; y++ {
			for x := cropRect.Min.X; x < cropRect.Max.X; x++ {
				rgbaImg.Set(x, y, img.At(x, y))
			}
		}
		return rgbaImg, nil
	} else {
		return nil, fmt.Errorf("cannot crop the image")
	}
}

func (c ControllerPicture) readPictures(ctx context.Context, projection *expression.ProjectionBuilder, filter *expression.ConditionBuilder) ([]controllerModel.Picture, error) {
	return c.DynamodbProcess.ReadPictures(ctx, c.PrimaryKey, projection, filter)
}
