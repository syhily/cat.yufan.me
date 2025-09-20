package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/h2non/bimg"
	"github.com/spf13/cobra"
	"golang.design/x/clipboard"
)

const (
	JPEG = "jpeg"
	JPG  = "jpg"
	PNG  = "png"
	AVIF = "avif"
	WEBP = "webp"
	GIF  = "gif"
	APNG = "apng"
	SVG  = "svg"
	BMP  = "bmp"
)

var supportExtensions = map[string]struct{}{
	JPEG: {},
	JPG:  {},
	PNG:  {},
	AVIF: {},
	WEBP: {},
	GIF:  {},
	APNG: {},
	SVG:  {},
	BMP:  {},
}

func init() {
	imageCmd.Flags().StringVarP(&imageSource, "source", "s", "", "The image file path (absolute of relative)")
	imageCmd.Flags().IntVarP(&width, "width", "", 1280, "The resized image width")
	imageCmd.Flags().IntVarP(&height, "height", "", 0, "The optional image height, 0 for keep ratio")
	imageCmd.Flags().StringVarP(&imageLocalDate, "time", "t", imageLocalDate, "The date time, in yyyyMMdd format")
	imageCmd.Flags().StringVarP(&imageFormat, "format", "f", JPG, "The image format")
	imageCmd.Flags().IntVarP(&imageQuality, "quality", "q", 0, "The image quality")

	err := imageCmd.MarkFlagRequired("source")
	if err != nil {
		log.Fatalf("%v", err)
	}

	rootCmd.AddCommand(imageCmd)
}

// imageCmd represents the image command
var (
	imageCmd = &cobra.Command{
		Use:   "image",
		Short: "A tool for processing images to my desired format, size and naming",
		Run: func(cmd *cobra.Command, args []string) {
			config := ReadConfig()

			// Check the image source path is valid.
			info, err := os.Stat(imageSource)
			if err != nil {
				log.Fatalf("Couldn't read the given file from the path %s, err: %v", imageSource, err)
			}

			if info.IsDir() {
				log.Fatalf("The given path %s is a directory. Only image is accepted", imageSource)
			}

			if ok, ext := isSupportedImage(info.Name()); !ok {
				log.Fatalf("Unsupported file extension %s. Allowed extensions: %s", ext, supportedFormats())
			}

			// Get the file operand
			img, err := os.Open(imageSource)
			if err != nil {
				log.Fatalf("Failed to read image %v", err)
			}

			// File convert format check.
			if _, ok := supportExtensions[imageFormat]; !ok {
				log.Fatalf("Invalid convert format, only supports %s", supportedFormats())
			}

			// Check the time pattern is valid.
			if !imageLocalDatePattern.Match([]byte(imageLocalDate)) {
				log.Fatalf("This is an invalid local date format %s", imageLocalDate)
			}
			t, err := time.Parse("20060102", imageLocalDate)
			if err != nil {
				log.Fatalf(`Invalid time str %v. It should be "yyyyMMdd"" like %v`, imageLocalDate, time.Now().Format("20060102"))
			}

			if imageQuality == 0 {
				imageQuality = config.Convert.DefaultQuality
			}
			if imageFormat == "" {
				imageFormat = config.Convert.DefaultFormat
			}

			process(img, width, height, imageFormat, imageQuality, t, config.ProjectRoot)
		},
	}

	width                 = 1280
	height                = 0
	imageSource           = ""
	imageLocalDate        = time.Now().Format("20060102")
	imageLocalDatePattern = regexp.MustCompile(`^\d{8}$`)
	imageFormat           = ""
	imageQuality          = 0
)

func supportedFormats() string {
	extensions := make([]string, 0, 10)
	for k := range supportExtensions {
		extensions = append(extensions, k)
	}

	return strings.Join(extensions, ", ")
}

func process(file *os.File, width, height int, format string, quality int, dt time.Time, target string) {
	bytes, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read the image %s\nError: %v", file.Name(), err)
	}

	// Image conversion.
	image := bimg.NewImage(bytes)
	it := imageType(format)
	options := bimg.Options{
		Width:   width,
		Height:  height,
		Crop:    false,
		Quality: quality,
		Rotate:  0,
		Type:    it,
	}
	size, err := image.Size()
	if err != nil {
		log.Fatalf("Image is invalid %v", err)
	}
	if height == 0 {
		options.Height = width * size.Height / size.Width
		options.Crop = false
	} else {
		options.Crop = true
	}
	bytes, err = image.Process(options)
	if err != nil {
		log.Fatalf("Failed to convert the images: %v", err)
	}

	// Create directory.
	directory := filepath.Join(target, "images", dt.Format("2006"), dt.Format("01"))
	err = os.MkdirAll(directory, os.FileMode(0755))
	if err != nil {
		log.Fatalf("Failed to create the image directory: %v", err)
	}

	// Save image file.
	filename := dt.Format("20060102") + time.Now().Format("150405") + fmt.Sprintf("%02d", time.Now().Nanosecond()%100) + "." + format
	file, err = os.OpenFile(filepath.Join(directory, filename), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(0644))
	if err != nil {
		log.Fatalf("Failed to generate the target image file: %v", filename)
	}
	writer := bufio.NewWriter(file)
	_, err = writer.Write(bytes)
	if err != nil {
		log.Fatalf("Failed to save image: %v", err)
	}

	log.Printf("The image is saved into the [%v]\n", filepath.Join(directory, filename))
	link, _ := url.JoinPath("https://cat.yufan.me/images", dt.Format("2006"), dt.Format("01"), filename)
	log.Printf("You can use link for document [%v]\n", link)

	// Save into clipboard
	clipboard.Write(clipboard.FmtText, []byte(link))
}

func isSupportedImage(name string) (bool, string) {
	ext := strings.ToLower(name[strings.LastIndex(name, ".")+1:])
	_, ok := supportExtensions[ext]
	return ok, ext
}

func imageType(format string) bimg.ImageType {
	switch format {
	case JPG:
	case JPEG:
		return bimg.JPEG
	case PNG:
		return bimg.PNG
	case AVIF:
		return bimg.AVIF
	case GIF:
		return bimg.GIF
	case APNG:
		return bimg.PNG
	case BMP:
		return bimg.JPEG
	case WEBP:
		return bimg.WEBP
	case SVG:
		return bimg.SVG
	}
	return bimg.JPEG
}
