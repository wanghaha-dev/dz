package dz

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"github.com/nfnt/resize"
)

const DZITemplate = `<?xml version="1.0" ?><Image Format="{{.Format}}" Overlap="{{.Overlap}}" TileSize="{{.TileSize}}" xmlns="http://schemas.microsoft.com/deepzoom/2009"><Size Height="{{.Height}}" Width="{{.Width}}"/></Image>`

var RESIZE_FILTERS = map[string]resize.InterpolationFunction{
	"bilinear": resize.Bilinear,
	"bicubic":  resize.Bicubic,
	"nearest":  resize.NearestNeighbor,
	"lanczos":  resize.Lanczos3,
}

func obtainFilter(filter string) string {
	b := false
	for k := range RESIZE_FILTERS {
		if filter == k {
			b = true
		}
	}
	if b == false {
		filter = "nearest"
	}
	return filter
}

func loadImage(filePath, format string) (image.Image, error) {
	fmt.Println("format:", format)
	if format == "" || format == "jpg" || format == "jpeg" {
		return gg.LoadJPG(filePath)
	} else if format == "png" {
		return gg.LoadPNG(filePath)
	} else {
		return nil, errors.New("only jpg and jpeg and png are supported")
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func pyramidLevelCheck(level int, dzid *DeepZoomImageDescriptor) {
	if level < 0 || level >= dzid.NumLevels() {
		panic(errors.New("invalid pyramid level"))
	}
}

func getOrCreatePath(filePath string) string {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		err := os.MkdirAll(filePath, os.ModePerm)
		check(err)
	}
	return filePath
}

func getFilesPath(filePath string) string {
	return strings.TrimSuffix(filePath, path.Ext(filePath)) + "_files"
}

type DeepZoomImageDescriptor struct {
	Format    string
	Overlap   int
	TileSize  int
	Height    int
	Width     int
	numLevels int
}

func (dzid *DeepZoomImageDescriptor) New(tileSize, overlap int, format string) *DeepZoomImageDescriptor {
	dzid.TileSize = tileSize
	dzid.Overlap = overlap
	dzid.Format = format
	return dzid
}

func (dzid *DeepZoomImageDescriptor) save(filePath string) {
	t := template.Must(template.New("dziTemplate").Parse(DZITemplate))
	writer, err := os.Create(filePath)
	check(err)
	err = t.Execute(writer, dzid)
	check(err)
}

func (dzid *DeepZoomImageDescriptor) NumLevels() int {
	if dzid.numLevels == 0 {
		maxDimension := int(math.Max(float64(dzid.Height), float64(dzid.Width)))
		dzid.numLevels = int(math.Ceil(math.Log2(float64(maxDimension)))) + 1
	}
	return dzid.numLevels
}

func (dzid *DeepZoomImageDescriptor) getScale(level int) float64 {
	pyramidLevelCheck(level, dzid)
	maxLevel := dzid.NumLevels() - 1
	return math.Pow(0.5, float64(maxLevel-level))
}

func (dzid *DeepZoomImageDescriptor) getDimensions(level int) (int, int) {
	pyramidLevelCheck(level, dzid)
	scale := dzid.getScale(level)
	width := int(math.Ceil(float64(dzid.Width) * scale))
	height := int(math.Ceil(float64(dzid.Height) * scale))
	return width, height
}

// Number of tiles (columns, rows)
func (dzid *DeepZoomImageDescriptor) getNumTiles(level int) (int, int) {
	pyramidLevelCheck(level, dzid)
	w, h := dzid.getDimensions(level)
	return int(math.Ceil(float64(w) / float64(dzid.TileSize))), int(math.Ceil(float64(h) / float64(dzid.TileSize)))
}

func (dzid *DeepZoomImageDescriptor) getTileBounds(level, column, row int) image.Rectangle {
	pyramidLevelCheck(level, dzid)
	offsetX, offsetY := 0, 0
	offsetW, offsetH := 1, 1
	if column != 0 {
		offsetX = dzid.Overlap
		offsetW = 2
	}
	if row != 0 {
		offsetY = dzid.Overlap
		offsetH = 2
	}
	x := (column * dzid.TileSize) - offsetX
	y := (row * dzid.TileSize) - offsetY
	levelWidth, levelHeight := dzid.getDimensions(level)
	w := dzid.TileSize + offsetW*dzid.Overlap
	h := dzid.TileSize + offsetH*dzid.Overlap
	w = int(math.Min(float64(w), float64(levelWidth-x)))
	h = int(math.Min(float64(h), float64(levelHeight-y)))
	return image.Rectangle{Min: image.Point{X: x, Y: y}, Max: image.Point{X: x + w, Y: y + h}}
}

type ImageCreator struct {
	dzid         *DeepZoomImageDescriptor
	Image        image.Image
	ImageQuality float64
	ResizeFilter string
	CopyMetadata bool
}

func (ic *ImageCreator) getImage(level int) image.Image {
	pyramidLevelCheck(level, ic.dzid)
	width, height := ic.dzid.getDimensions(level)
	if ic.dzid.Width == width && ic.dzid.Height == height {
		return ic.Image
	}
	filter := obtainFilter(ic.ResizeFilter)
	return resize.Thumbnail(uint(width), uint(height), ic.Image, RESIZE_FILTERS[filter])
}

// New Creates Deep Zoom image from source file
func (ic *ImageCreator) New(source, format string, tileSize, overlap int) (*ImageCreator, error) {
	img, err := loadImage(source, format)
	if err != nil {
		return nil, err
	}
	ic.Image = img
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	ic.dzid = &DeepZoomImageDescriptor{Width: width, Height: height, TileSize: tileSize, Overlap: overlap, Format: format}
	return ic, nil
}

func (ic *ImageCreator) create(destination string) {
	imageFiles := getOrCreatePath(getFilesPath(destination))
	format := ic.dzid.Format
	for level := 0; level < ic.dzid.NumLevels(); level++ {
		levelDir := getOrCreatePath(path.Join(imageFiles, strconv.Itoa(level)))
		levelImage := ic.getImage(level)
		columns, rows := ic.dzid.getNumTiles(level)
		for column := 0; column < columns; column++ {
			for row := 0; row < rows; row++ {
				go func(ic *ImageCreator, levelImage *image.Image, levelDir, format string, level, column, row int) {
					bounds := ic.dzid.getTileBounds(level, column, row)
					tile := imaging.Crop(*levelImage, bounds)
					tilePath := path.Join(levelDir, fmt.Sprintf("%d_%d.%s", column, row, format))
					out, err := os.Create(tilePath)
					check(err)
					err = jpeg.Encode(out, tile, &jpeg.Options{Quality: int(ic.ImageQuality * 100)})
					check(err)
				}(ic, &levelImage, levelDir, format, level, column, row)
			}
		}
	}
	ic.dzid.save(destination)
}

// CreateDzi create dzi
func CreateDzi(source string, format string, destination string) error {
	creator := new(ImageCreator)

	// output image quality
	creator.ImageQuality = 0.8
	tileSize := 254
	overlap := 1
	_, err := creator.New(source, format, tileSize, overlap)
	if err != nil {
		return err
	}

	// destination of dzi file path
	creator.create(destination)
	return nil
}
