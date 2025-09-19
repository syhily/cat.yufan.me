package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

func init() {
	rootCmd.AddCommand(configCmd)

	rootCmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigRoot(), "The config file directory")
}

const (
	ConfigFileName = "gifts.yml"
)

var (
	configCmd = &cobra.Command{
		Use:   "config",
		Short: "Generate a global configuration file for pandora tool",
		Run: func(cmd *cobra.Command, args []string) {
			stat, err := os.Stat(configPath)
			if errors.Is(err, os.ErrNotExist) {
				err = os.MkdirAll(configPath, os.FileMode(0755))
				if err != nil {
					log.Fatalf("Failed to create the config path %s\nError: %v", configPath, err)
				}
			} else if err != nil || !stat.IsDir() {
				log.Fatalf("Invalid config path %s.", configPath)
			}

			configFile := filepath.Join(configPath, ConfigFileName)
			file, err := os.OpenFile(configFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0644))
			if err != nil {
				log.Fatalf("Failed to create config file %s\nError: %v", configFile, err)
			}
			writer := bufio.NewWriter(file)

			var (
				projectRoot       string
				convertQuality    int
				convertFormat     string
				s3Region          string
				s3Endpoint        string
				s3Bucket          string
				s3AccessKey       string
				s3AccessSecretKey string
			)

			executeRoot, _ := os.Getwd()
			fmt.Printf("Please input the project root. Default [.]")
			_, _ = fmt.Scanln(&projectRoot)
			if projectRoot == "" {
				projectRoot = executeRoot
			}

			fmt.Println("Please input the convert quality. Default [75]")
			_, _ = fmt.Scanf("%d", &convertQuality)
			if convertQuality == 0 {
				convertQuality = 75
			}

			fmt.Println("Please input the convert format. Default [jpg]")
			_, _ = fmt.Scanln(&convertFormat)
			if convertFormat == "" {
				convertFormat = JPG
			} else {
				if _, ok := supportExtensions[convertFormat]; !ok {
					log.Fatalf("Unsupported convert format: %s", convertFormat)
				}
			}

			fmt.Println("Please input the s3 region (Optional)")
			_, _ = fmt.Scanln(&s3Region)

			fmt.Println("Please input the s3 endpoint (Optional)")
			_, _ = fmt.Scanln(&s3Endpoint)

			fmt.Println("Please input the s3 bucket")
			_, _ = fmt.Scanln(&s3Bucket)

			fmt.Println("Please input the s3 access key")
			_, _ = fmt.Scanln(&s3AccessKey)

			fmt.Println("Please input the s3 access secret key")
			_, _ = fmt.Scanln(&s3AccessSecretKey)

			var cs = PandoraConfig{
				ProjectRoot: projectRoot,
				Convert: struct {
					DefaultQuality int    `yaml:"defaultQuality"`
					DefaultFormat  string `yaml:"defaultFormat"`
				}{
					DefaultQuality: convertQuality,
					DefaultFormat:  convertFormat,
				},
				S3: struct {
					Region          string `yaml:"region"`
					Endpoint        string `yaml:"endpoint"`
					Bucket          string `yaml:"bucket"`
					AccessKey       string `yaml:"accessKey"`
					AccessSecretKey string `yaml:"accessSecretKey"`
				}{
					Region:          s3Region,
					Endpoint:        s3Endpoint,
					Bucket:          s3Bucket,
					AccessKey:       s3AccessKey,
					AccessSecretKey: s3AccessSecretKey,
				},
			}

			encoder := yaml.NewEncoder(writer)
			encoder.SetIndent(2)
			err = encoder.Encode(&cs)
			if err != nil {
				log.Fatalf("Failed to generate the configuration file: %v\nError: %v", configFile, err)
			}

			err = writer.Flush()
			if err != nil {
				log.Fatalf("Failed to write config file: %v", err)
			}
		},
	}
	configPath string
)

type PandoraConfig struct {
	// The root file for storing the images
	ProjectRoot string `yaml:"projectRoot"`
	Convert     struct {
		DefaultQuality int    `yaml:"defaultQuality"`
		DefaultFormat  string `yaml:"defaultFormat"`
	} `yaml:"convert"`
	S3 struct {
		Region          string `yaml:"region"`
		Endpoint        string `yaml:"endpoint"`
		Bucket          string `yaml:"bucket"`
		AccessKey       string `yaml:"accessKey"`
		AccessSecretKey string `yaml:"accessSecretKey"`
	} `yaml:"s3"`
}

func (c *PandoraConfig) Retrieve(context.Context) (aws.Credentials, error) {
	if c.S3.AccessKey == "" || c.S3.AccessSecretKey == "" {
		return aws.Credentials{}, fmt.Errorf("no accessKey or AccessSecretKey is provided")
	}

	return aws.Credentials{
		AccessKeyID:     c.S3.AccessKey,
		SecretAccessKey: c.S3.AccessSecretKey,
	}, nil
}

func DefaultConfigRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to read user home directory %v", err)
	}
	return filepath.Join(home, ".config", "pandora")
}

// ReadConfig will load the yaml based configuration file and deserialize it into the target path.
func ReadConfig() *PandoraConfig {
	// Initialize pandora config
	stat, err := os.Stat(configPath)
	if err != nil || !stat.IsDir() {
		log.Fatalf(`It sees like you haven't config the tool.\nExecute the command "pandora config" for initializing.`)
	}

	file, err := os.Open(filepath.Join(configPath, ConfigFileName))
	if err != nil {
		log.Fatalf("Failed to load the config file from: %s.\nError: %v", configPath, err)
	}

	reader := bufio.NewReader(file)
	decoder := yaml.NewDecoder(reader)

	var c PandoraConfig
	err = decoder.Decode(&c)
	if err != nil {
		log.Fatalf("Invalid config file format or location %s.\nError: %v", configPath, err)
	}
	return &c
}
