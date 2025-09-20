package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/spf13/cobra"
)

const (
	BlurDataFormat    = `data:image/webp;base64,%s`
	ImageMetadataFile = "images/metadata.json"
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
			for _, directory := range []string{"images", "uploads"} {
				metas = append(metas, SyncDirectory(client, filepath.Join(config.ProjectRoot, directory))...)
			}

			UploadMetadata(client, config, metas)
		},
	}
)

func init() {
	rootCmd.AddCommand(syncCmd)
}

func SyncDirectory(client *BucketClient, directory string) []ImageMetadata {
	var metas []ImageMetadata
	// TODO
	return metas
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

func newBucketClient(config *PandoraConfig) *BucketClient {
	client := s3.NewFromConfig(aws.Config{
		Region:       config.S3.Region,
		BaseEndpoint: aws.String(config.S3.Endpoint),
		Credentials:  config,
	})
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
func (bucket BucketClient) UploadObject(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	file, err := os.Open(fileName)
	if err != nil {
		log.Printf("Couldn't open file %v to upload. Here's why: %v\n", fileName, err)
	} else {
		defer func() { _ = file.Close() }()
		_, err = bucket.Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
			Body:   file,
		})
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
				log.Printf("Error while uploading object to %s. The object is too large.\n"+
					"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
					"or the multipart upload API (5TB max).", bucketName)
			} else {
				log.Printf("Couldn't upload file %v to %v:%v. Here's why: %v\n",
					fileName, bucketName, objectKey, err)
			}
		} else {
			err = s3.NewObjectExistsWaiter(bucket.Client).Wait(
				ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
			if err != nil {
				log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
			}
		}
	}
	return err
}

// ListObjects lists the objects in a bucket.
func (bucket BucketClient) ListObjects(ctx context.Context, bucketName string) ([]types.Object, error) {
	var err error
	var output *s3.ListObjectsV2Output
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
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
