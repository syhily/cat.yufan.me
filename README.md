# UPYUN Image Store

This is a repository for storing and uploading the images to [UPYUN](https://cdn.yufan.me).

## Configure Tools

```text
pandora config -h
Generate a global configuration file for pandora tool

Usage:
  pandora config [flags]

Flags:
  -h, --help   help for config
```

## Convert Images

```text
pandora image -h
A tool for processing images to my desired format, size and naming

Usage:
  pandora image [flags]

Flags:
  -f, --format string   The image format (default "jpg")
      --height int      The optional image height, 0 for keep ratio
  -h, --help            help for image
  -q, --quality int     The image quality
  -s, --source string   The image file path (absolute of relative)
  -t, --time string     The date time, in yyyyMMdd format (default "20250920")
      --width int       The resized image width (default 1280)
```

### Upload Images

```text
pandora sync -h
A tool for syncing files to UPYUN. A metadata file will be generated to track the synced files.

Usage:
  pandora sync [flags]

Flags:
  -h, --help   help for sync
```
