package router

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/foolin/pagser"

	"path/filepath"

	"scraper/src/mongodb"
	"scraper/src/types"
	"scraper/src/utils"

	"github.com/jinzhu/copier"

	"golang.org/x/exp/slices"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"regexp"
	"strings"
)

type ParamsSearchPhotoFlickr struct {
	Quality string `uri:"quality" binding:"required"`
}

// Find all the photos with specific quality and folder directory.
func SearchPhotosFlickr(s3Client *s3.Client, mongoClient *mongo.Client, params ParamsSearchPhotoFlickr) ([]primitive.ObjectID, error) {

	quality := params.Quality
	qualitiesAvailable := []string{"Small", "Medium", "Large", "Original"}
	idx := slices.IndexFunc(qualitiesAvailable, func(qualityAvailable string) bool { return qualityAvailable == quality })
	if idx == -1 {
		return nil, fmt.Errorf("quality needs to be `Original`(w=2400), `Large`(w=1024), `Medium`(w = 500) or `Small`(w = 240) and your is `%s`", quality)
	}
	var insertedIDs []primitive.ObjectID
	parser := pagser.New() // parsing html in string responses

	origin := "flickr"

	collectionImagesPending := mongoClient.Database(utils.GetEnvVariable("SCRAPER_DB")).Collection(utils.GetEnvVariable("IMAGES_PENDING_COLLECTION"))
	collectionImagesWanted := mongoClient.Database(utils.GetEnvVariable("SCRAPER_DB")).Collection(utils.GetEnvVariable("IMAGES_WANTED_COLLECTION"))
	collectionImagesUnwanted := mongoClient.Database(utils.GetEnvVariable("SCRAPER_DB")).Collection(utils.GetEnvVariable("IMAGES_UNWANTED_COLLECTION"))
	collectionUsersUnwanted := mongoClient.Database(utils.GetEnvVariable("SCRAPER_DB")).Collection(utils.GetEnvVariable("USERS_UNWANTED_COLLECTION"))

	unwantedTags, wantedTags, err := mongodb.TagsNames(mongoClient)
	if err != nil {
		return nil, err
	}

	for _, wantedTag := range wantedTags {

		// all the commercial use licenses
		// https://www.flickr.com/services/api/flickr.photos.licenses.getInfo.html
		var licenseIDsNames = map[string]string{
			"4":  "Attribution License",
			"5":  "Attribution-ShareAlike License",
			"7":  "No known copyright restrictions",
			"9":  "Public Domain Dedication (CC0)",
			"10": "Public Domain Mark",
		}
		licenseIDs := [5]string{"4", "5", "7", "9", "10"}
		for _, licenseID := range licenseIDs {

			// start with the first page
			page := 1
			searchPerPage, err := searchPhotosPerPageFlickr(parser, licenseID, wantedTag, fmt.Sprint(page))
			if err != nil {
				return nil, fmt.Errorf("searchPhotosPerPageFlickr has failed: %v", err)
			}

			for page := page; page <= int(searchPerPage.Pages); page++ {
				searchPerPage, err := searchPhotosPerPageFlickr(parser, licenseID, wantedTag, fmt.Sprint(page))
				if err != nil {
					return nil, fmt.Errorf("searchPhotosPerPageFlickr has failed: %v", err)
				}
				for _, photo := range searchPerPage.Photos {
					// look for existing images
					query := bson.M{"originID": photo.ID}
					options := options.FindOne().SetProjection(bson.M{"_id": 1})
					imagePendingFound, err := mongodb.FindOne[types.Image](collectionImagesPending, query, options)
					if err != nil {
						return nil, fmt.Errorf("FindOne[Image] pending existing image has failed: %v", err)
					}
					if imagePendingFound != nil {
						continue // skip existing wanted image
					}
					imageWantedFound, err := mongodb.FindOne[types.Image](collectionImagesWanted, query, options)
					if err != nil {
						return nil, fmt.Errorf("FindOne[Image] wanted existing image has failed: %v", err)
					}
					if imageWantedFound != nil {
						continue // skip existing pending image
					}
					imageUnwantedFound, err := mongodb.FindOne[types.Image](collectionImagesUnwanted, query, options)
					if err != nil {
						return nil, fmt.Errorf("FindOne[Image] unwanted existing image has failed: %v", err)
					}
					if imageUnwantedFound != nil {
						continue // skip image unwanted
					}

					// extract the photo informations
					infoData, err := infoPhoto(parser, photo)
					if err != nil {
						return nil, fmt.Errorf("InfoPhoto has failed: %v", err)
					}
					if (infoData.OriginalFormat == "jpeg"){
						infoData.OriginalFormat = "jpg"
					}

					// look for unwanted Users
					query = bson.M{"origin": origin,
						"$or": bson.A{
							bson.M{"originID": infoData.UserID},
							bson.M{"name": infoData.UserName},
						},
					}
					userFound, err := mongodb.FindOne[types.User](collectionUsersUnwanted, query)
					if err != nil {
						return nil, fmt.Errorf("FindOne[User] has failed: %v", err)
					}
					if userFound != nil {
						continue // skip the image with unwanted user
					}

					var photoTags []string
					for _, tag := range infoData.Tags {
						photoTags = append(photoTags, strings.ToLower(tag.Name))
					}

					// skip image if one of its tag is unwanted
					idx := utils.FindIndexRegExp(unwantedTags, photoTags)
					if idx != -1 {
						continue // skip image with unwanted tag
					}

					// extract the photo download link
					downloadData, err := downloadPhoto(parser, photo.ID)
					if err != nil {
						return nil, fmt.Errorf("DownloadPhoto has failed: %v", err)
					}

					// get the download link for the correct resolution
					label := strings.ToLower(quality)
					regexpMatch := fmt.Sprintf(`[\-\_\w\d]*%s[\-\_\w\d]*`, label)
					idx = slices.IndexFunc(downloadData.Photos, func(download DownloadPhotoSingleData) bool { return strings.ToLower(download.Label) == label })
					if idx == -1 {
						idx = slices.IndexFunc(downloadData.Photos, func(download DownloadPhotoSingleData) bool {
							matched, err := regexp.Match(regexpMatch, []byte(strings.ToLower(download.Label)))
							if err != nil {
								return false
							}
							return matched
						})
					}
					if idx == -1 {
						return nil, fmt.Errorf("cannot find label %s and its derivatives %s in SearchPhoto! id %s has available the following:%v", label, regexpMatch, photo.ID, downloadData)
					}

					// get the file and rename it <id>.<format>
					fileName := fmt.Sprintf("%s.%s", photo.ID, infoData.OriginalFormat)
					path := filepath.Join(origin, fileName)

					// get buffer of image
					buffer, err := utils.GetFile(downloadData.Photos[idx].Source)
					if err != nil {
						return nil, fmt.Errorf("GetFile has failed: %v", err)
					}

					_, err = utils.UploadItemS3(s3Client, bytes.NewReader(buffer), path)
					if err != nil {
						return nil, fmt.Errorf("UploadItemS3 has failed: %v", err)
					}

					// image creation
					imageSizeID := primitive.NewObjectID()
					var tags []types.Tag
					copier.Copy(&tags, &infoData.Tags)
					now := time.Now()
					for i := 0; i < len(tags); i++ {
						tag := &tags[i]
						tag.Name = strings.ToLower(tag.Name)
						tag.CreationDate = &now
						tag.Origin.Name = origin
						tag.Origin.ImageSizeID = imageSizeID
					}
					user := types.User{
						Origin:       origin,
						Name:         infoData.UserName,
						OriginID:     infoData.UserID,
						CreationDate: &now,
					}
					zero := 0
					box := types.Box{
						Tlx:      &zero, // original x anchor
						Tly:      &zero, // original y anchor
						Width:  &downloadData.Photos[idx].Width,
						Height: &downloadData.Photos[idx].Height,
					}
					size := []types.ImageSize{{
						ID:           imageSizeID,
						CreationDate: &now,
						Box:          box,
					}}
					document := types.Image{
						Origin:       origin,
						OriginID:     photo.ID,
						User:         user,
						Extension:    infoData.OriginalFormat,
						Name:         photo.ID,
						Size:         size,
						Title:        infoData.Title,
						Description:  infoData.Description,
						License:      licenseIDsNames[licenseID],
						CreationDate: &now,
						Tags:         tags,
					}
					insertedID, err := mongodb.InsertImage(collectionImagesPending, document)
					if err != nil {
						return nil, fmt.Errorf("InsertImage has failed: %v", err)
					}
					insertedIDs = append(insertedIDs, insertedID)
				}
			}
		}
	}
	return insertedIDs, nil
}

// https://golangexample.com/pagser-a-simple-and-deserialize-html-page-to-struct-based-on-goquery-and-struct-tags-for-golang-crawler/
type SearchPhotPerPageData struct {
	Stat    string        `pagser:"rsp->attr(stat)"`
	Page    uint          `pagser:"photos->attr(page)"`
	Pages   uint          `pagser:"photos->attr(pages)"`
	PerPage uint          `pagser:"photos->attr(perpage)"`
	Total   uint          `pagser:"photos->attr(total)"`
	Photos  []PhotoFlickr `pagser:"photo"`
}
type PhotoFlickr struct {
	ID     string `pagser:"->attr(id)"`
	Secret string `pagser:"->attr(secret)"`
	Title  string `pagser:"->attr(title)"`
}

// Search images for one page of max 500 images
func searchPhotosPerPageFlickr(parser *pagser.Pagser, licenseID string, tags string, page string) (*SearchPhotPerPageData, error) {
	r := &utils.Request{
		Host: "https://api.flickr.com/services/rest/?",
		Args: map[string]string{
			"api_key":  utils.GetEnvVariable("FLICKR_PUBLIC_KEY"),
			"method":   "flickr.photos.search",
			"tags":     tags,
			"license":  licenseID,
			"media":    "photos",
			"per_page": "500", // 100 default, max 500
			"page":     page,
		},
	}
	// fmt.Println(r.URL())

	body, err := r.ExecuteGET()
	if err != nil {
		return nil, err
	}

	var pageData SearchPhotPerPageData
	err = parser.Parse(&pageData, string(body))
	if err != nil {
		return nil, err
	}
	if pageData.Stat != "ok" {
		return nil, fmt.Errorf("SearchPhotoPerPageRequest is not ok%v", pageData)
	}
	if pageData.Page == 0 || pageData.Pages == 0 || pageData.PerPage == 0 || pageData.Total == 0 {
		return nil, errors.New("some informations are missing from SearchPhotoPerPage")
	}
	return &pageData, nil
}

// https://golangexample.com/pagser-a-simple-and-deserialize-html-page-to-struct-based-on-goquery-and-struct-tags-for-golang-crawler/
type DownloadPhotoSingleData struct {
	Label  string `pagser:"->attr(label)"`
	Width  int    `pagser:"->attr(width)"`
	Height int    `pagser:"->attr(height)"`
	Source string `pagser:"->attr(source)"`
}

// https://golangexample.com/pagser-a-simple-and-deserialize-html-page-to-struct-based-on-goquery-and-struct-tags-for-golang-crawler/
type DownloadPhotoData struct {
	Stat   string                    `pagser:"rsp->attr(stat)"`
	Photos []DownloadPhotoSingleData `pagser:"size"`
}

func downloadPhoto(parser *pagser.Pagser, id string) (*DownloadPhotoData, error) {
	r := &utils.Request{
		Host: "https://api.flickr.com/services/rest/?",
		Args: map[string]string{
			"api_key":  utils.GetEnvVariable("FLICKR_PUBLIC_KEY"),
			"method":   "flickr.photos.getSizes",
			"photo_id": id,
		},
	}
	// fmt.Println(r.URL())

	body, err := r.ExecuteGET()
	if err != nil {
		return nil, fmt.Errorf("DownloadPhoto has failed: %v", err)
	}

	var downloadData DownloadPhotoData
	err = parser.Parse(&downloadData, string(body))
	if err != nil {
		return nil, err
	}

	if downloadData.Stat != "ok" {
		return nil, fmt.Errorf("DownloadPhoto is not ok%v", downloadData)
	}

	return &downloadData, nil
}

// https://golangexample.com/pagser-a-simple-and-deserialize-html-page-to-struct-based-on-goquery-and-struct-tags-for-golang-crawler/
type InfoPhotoData struct {
	Stat           string `pagser:"rsp->attr(stat)"`
	ID             string `pagser:"photo->attr(id)"`
	Secret         string `pagser:"photo->attr(secret)"`
	OriginalSecret string `pagser:"photo->attr(originalsecret)"`
	OriginalFormat string `pagser:"photo->attr(originalformat)"`
	Title          string `pagser:"title"`
	Description    string `pagser:"description"`
	UserID         string `pagser:"owner->attr(nsid)"`
	UserName       string `pagser:"owner->attr(username)"`
	Tags           []Tag  `pagser:"tag"`
}

type Tag struct {
	Name string `pagser:"->text()"`
}

func infoPhoto(parser *pagser.Pagser, photo PhotoFlickr) (*InfoPhotoData, error) {
	r := &utils.Request{
		Host: "https://api.flickr.com/services/rest/?",
		Args: map[string]string{
			"api_key":  utils.GetEnvVariable("FLICKR_PUBLIC_KEY"),
			"method":   "flickr.photos.getInfo",
			"photo_id": photo.ID,
		},
	}
	// fmt.Println(r.URL())

	body, err := r.ExecuteGET()
	if err != nil {
		return nil, err
	}

	var infoData InfoPhotoData
	err = parser.Parse(&infoData, string(body))
	if err != nil {
		return nil, err
	}

	if infoData.Stat != "ok" {
		return nil, fmt.Errorf("InfoPhoto is not ok%v", infoData)
	}
	if photo.ID != infoData.ID {
		return nil, fmt.Errorf("IDs do not match! search id: %s, info id: %s", photo.ID, infoData.ID)
	}
	if photo.Secret != infoData.Secret {
		return nil, fmt.Errorf("secrets do not match for id: %s! search secret: %s, info secret: %s", photo.ID, photo.Secret, infoData.Secret)
	}
	return &infoData, nil
}
