package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/spf13/cobra"
)

var (
	syncCmd = &cobra.Command{
		Use:   "sync",
		Short: "A tool for syncing files to UPYUN. A metadata file will be generated to track the synced files.",
		Run: func(cmd *cobra.Command, args []string) {
			config := ReadConfig()
			client := newBucketClient(config)

			SyncDirectories(client, config, "images", "uploads")
		},
	}
)

func init() {
	rootCmd.AddCommand(syncCmd)
}

func SyncDirectories(client *BucketClient, config *PandoraConfig, directories ...string) {
	// Upload the files into the S3.

	// Generate the image metadata JSON.
}

func newBucketClient(config *PandoraConfig) *BucketClient {
	client := s3.NewFromConfig(aws.Config{
		Region:       config.S3.Region,
		BaseEndpoint: &config.S3.Endpoint,
		Credentials:  config,
	})
	return &BucketClient{client: client}
}

// BucketClient encapsulates the Amazon Simple Storage Service (Amazon S3) actions
// used in the sync command.
// It contains client, an Amazon S3 service client that is used to perform bucket
// and object actions.
type BucketClient struct {
	client *s3.Client
}

// UploadObject reads from a file and puts the data into an object in a bucket.
func (bucket BucketClient) UploadObject(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	file, err := os.Open(fileName)
	if err != nil {
		log.Printf("Couldn't open file %v to upload. Here's why: %v\n", fileName, err)
	} else {
		defer func() { _ = file.Close() }()
		_, err = bucket.client.PutObject(ctx, &s3.PutObjectInput{
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
			err = s3.NewObjectExistsWaiter(bucket.client).Wait(
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
	objectPaginator := s3.NewListObjectsV2Paginator(bucket.client, input)
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
	output, err := bucket.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
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
			err = s3.NewObjectNotExistsWaiter(bucket.client).Wait(
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
