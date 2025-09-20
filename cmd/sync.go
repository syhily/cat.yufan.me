package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	endpoints "github.com/aws/smithy-go/endpoints"
	"github.com/h2non/bimg"
	"github.com/spf13/cobra"
)

const (
	BlurDataFormat    = `data:image/webp;base64,%s`
	ImageMetadataFile = "images/metadata.json"
	BlurWidth         = 8
)

var (
	syncCmd = &cobra.Command{
		Use:   "sync",
		Short: "A tool for syncing files to UPYUN. A metadata file will be generated to track the synced files.",
		Run: func(cmd *cobra.Command, args []string) {
			// Create S3 client.
			config := ReadConfig()
			client := newBucketClient(config)

			// Upload the files into the S3.
			var metas []ImageMetadata
			ctx := context.TODO()
			for _, directory := range []string{"images", "uploads"} {
				r := SyncDirectory(ctx, client, config, filepath.Join(config.ProjectRoot, directory))
				if r != nil {
					metas = append(metas, r...)
				}
			}
			log.Println("Successfully sync the directories")

			// Upload the generated image metadata.
			log.Println("Generate the image metadata")
			UploadMetadata(client, config, metas)
			log.Println("Successfully upload the image metadata")
		},
	}
)

func init() {
	rootCmd.AddCommand(syncCmd)
}

func SyncDirectory(ctx context.Context, client *BucketClient, config *PandoraConfig, directory string) []ImageMetadata {
	var metas []ImageMetadata

	if stat, err := os.Stat(directory); err != nil {
		log.Printf("Failed to read current directory %v", directory)
		return metas
	} else if stat.IsDir() && !strings.HasPrefix(stat.Name(), ".") {
		// Load the files/directories from the current directory.
		files, e := os.ReadDir(directory)
		if e != nil {
			return metas
		}

		// Load the path prefix from AWS s3.
		objs, e := client.ListObjects(ctx, config.S3.Bucket, directory[len(config.ProjectRoot):])
		if e != nil {
			log.Printf("Failed to read directory from S3: %v\nError: %v", directory[len(config.ProjectRoot):], e)
		}
		awsMetas := map[string]int64{}
		for _, obj := range objs {
			awsMetas[*obj.Key] = *obj.Size
		}

		// Range the files into the current directory.
		for _, file := range files {
			if strings.HasPrefix(file.Name(), ".") {
				continue
			} else if file.IsDir() {
				m := SyncDirectory(ctx, client, config, filepath.Join(directory, file.Name()))
				if m != nil {
					metas = append(metas, m...)
				}
			} else {
				// Upload the file if the file size is mismatch.
				info, e1 := file.Info()
				if e1 != nil {
					log.Printf("Failed to read the file %v info", file.Name())
					continue
				}

				filename := filepath.Join(directory, file.Name())
				content, e2 := os.ReadFile(filename)
				log.Println("Try to sync file", filename)

				if info.Size() != awsMetas[file.Name()] {
					if e2 != nil {
						log.Printf("Failed to read the file %v content", filename)
						continue
					}

					e2 = client.UploadObject(ctx, config.S3.Bucket, filename[len(config.ProjectRoot):], content)
					if e2 != nil {
						log.Printf("Failed to upload the file %v to s3", filename)
						continue
					}
				}

				if ok, _ := isSupportedImage(file.Name()); ok {
					meta := ReadImageMetadata(filename, filename[len(config.ProjectRoot):], content)
					if meta != nil {
						metas = append(metas, *meta)
					}
				}
			}
		}
	}

	return metas
}

func ReadImageMetadata(file, key string, content []byte) *ImageMetadata {
	if ok, _ := isSupportedImage(file); ok {
		image := bimg.NewImage(content)
		size, err := image.Size()
		if err != nil {
			log.Printf("Failed to read the image size for %v", file)
			return nil
		}
		options := bimg.Options{
			Width:   BlurWidth,
			Height:  size.Height * BlurWidth / size.Width,
			Crop:    false,
			Quality: 1,
			Rotate:  0,
			Type:    bimg.WEBP,
		}
		b, err := image.Process(options)
		if err != nil {
			log.Printf("Failed to generate the blur image %v", err)
			return nil
		}
		blur := base64.StdEncoding.EncodeToString(b)
		return &ImageMetadata{
			Path:        key,
			Width:       size.Width,
			Height:      size.Height,
			BlurDataURL: fmt.Sprintf(BlurDataFormat, blur),
		}
	}
	return nil
}

type ImageMetadata struct {
	Path        string `json:"path"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	BlurDataURL string `json:"blurDataURL"`
}

func UploadMetadata(bucket *BucketClient, config *PandoraConfig, metadata []ImageMetadata) {
	var buf = new(bytes.Buffer)
	encoder := json.NewEncoder(buf)
	err := encoder.Encode(&metadata)
	if err != nil {
		log.Fatalf("Failed to generate the JSON file for image metadatas.")
	}

	// Upload the metadata JSON
	ctx := context.TODO()

	_, err = bucket.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(config.S3.Bucket),
		Key:    aws.String(ImageMetadataFile),
		Body:   buf,
	})
	if err != nil {
		log.Printf("Couldn't upload image meta file. Here's why: %v\n", err)
	} else {
		err = s3.NewObjectExistsWaiter(bucket.Client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(config.S3.Bucket), Key: aws.String(ImageMetadataFile)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for image meta file %s to exist.\n", ImageMetadataFile)
		}
	}
}

type resolverV2 struct{}

func (*resolverV2) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (endpoints.Endpoint, error) {
	return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
}

func newBucketClient(config *PandoraConfig) *BucketClient {
	var client *s3.Client
	if config.S3.Region != "" {
		client = s3.NewFromConfig(aws.Config{
			Region:      config.S3.Region,
			Credentials: config,
		})
	} else {
		client = s3.NewFromConfig(aws.Config{
			Region:      "auto",
			Credentials: config,
		}, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(config.S3.Endpoint)
			o.EndpointResolverV2 = &resolverV2{}
		})
	}
	return &BucketClient{Client: client}
}

// BucketClient encapsulates the Amazon Simple Storage Service (Amazon S3) actions
// used in the sync command.
// It contains client, an Amazon S3 service client that is used to perform bucket
// and object actions.
type BucketClient struct {
	Client *s3.Client
}

// UploadObject reads from a file and puts the data into an object in a bucket.
func (bucket BucketClient) UploadObject(ctx context.Context, bucketName string, objectKey string, content []byte) error {
	_, err := bucket.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(content),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
			log.Printf("Error while uploading object to %s. The object is too large.\n"+
				"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
				"or the multipart upload API (5TB max).", bucketName)
		} else {
			log.Printf("Couldn't upload file to %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		}
	} else {
		err = s3.NewObjectExistsWaiter(bucket.Client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
		}
	}
	return err
}

// ListObjects lists the objects in a bucket.
func (bucket BucketClient) ListObjects(ctx context.Context, bucketName string, key string) ([]types.Object, error) {
	var err error
	var output *s3.ListObjectsV2Output
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(key),
	}
	var objects []types.Object
	objectPaginator := s3.NewListObjectsV2Paginator(bucket.Client, input)
	for objectPaginator.HasMorePages() {
		output, err = objectPaginator.NextPage(ctx)
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucketName)
				err = noBucket
			}
			break
		} else {
			objects = append(objects, output.Contents...)
		}
	}
	return objects, err
}

// DeleteObjects deletes a list of objects from a bucket.
func (bucket BucketClient) DeleteObjects(ctx context.Context, bucketName string, objectKeys []string) error {
	var objectIds []types.ObjectIdentifier
	for _, key := range objectKeys {
		objectIds = append(objectIds, types.ObjectIdentifier{Key: aws.String(key)})
	}
	output, err := bucket.Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucketName),
		Delete: &types.Delete{Objects: objectIds, Quiet: aws.Bool(true)},
	})
	if err != nil || len(output.Errors) > 0 {
		log.Printf("Error deleting objects from bucket %s.\n", bucketName)
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucketName)
				err = noBucket
			}
		} else if len(output.Errors) > 0 {
			for _, outErr := range output.Errors {
				log.Printf("%s: %s\n", *outErr.Key, *outErr.Message)
			}
			err = fmt.Errorf("%s", *output.Errors[0].Message)
		}
	} else {
		for _, delObjects := range output.Deleted {
			err = s3.NewObjectNotExistsWaiter(bucket.Client).Wait(
				ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: delObjects.Key}, time.Minute)
			if err != nil {
				log.Printf("Failed attempt to wait for object %s to be deleted.\n", *delObjects.Key)
			} else {
				log.Printf("Deleted %s.\n", *delObjects.Key)
			}
		}
	}
	return err
}
