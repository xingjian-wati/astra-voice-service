package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/gcs"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/jung-kurt/gofpdf/v2"
	"go.uber.org/zap"
)

// findNotoFont returns the absolute path to Noto Sans SC font if found
func findNotoFont() string {
	cwd, err := os.Getwd()
	if err != nil {
		logger.Base().Error("Failed to get working directory")
		return ""
	}

	fontPath := filepath.Join(cwd, "static", "fonts", "NotoSansSC-Regular.ttf")
	fontPath = filepath.Clean(fontPath)

	logger.Base().Debug("Looking for font at: (cwd: )", zap.String("fontpath", fontPath), zap.String("cwd", cwd))

	// Verify file exists
	if _, err := os.Stat(fontPath); err != nil {
		logger.Base().Error("Font file not found at: (error: )", zap.String("fontpath", fontPath))
		return ""
	}

	// Get absolute path
	absPath, err := filepath.Abs(fontPath)
	if err != nil {
		logger.Base().Error("Failed to get absolute path")
		return ""
	}

	// Ensure path starts with / (Unix path requirement)
	// This is critical - gofpdf requires paths starting with /
	if !strings.HasPrefix(absPath, "/") {
		absPath = "/" + absPath
		logger.Base().Debug("Fixed absolute path to start with /", zap.String("abspath", absPath))
	}

	// Final verification
	if !strings.HasPrefix(absPath, "/") {
		logger.Base().Error("Path still doesn't start with / after fix", zap.String("abspath", absPath))
		return ""
	}

	logger.Base().Info("Font path verified", zap.String("abspath", absPath))
	return absPath
}

// PDFParams holds parameters for PDF generation
type PDFParams struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Filename string `json:"filename,omitempty"`
}

// GeneratePDFFromText generates a PDF file from text content
func GeneratePDFFromText(title, content, filename string) (map[string]interface{}, error) {
	logger.Base().Info("Generating PDF", zap.String("title", title), zap.String("filename", filename), zap.Int("content_length", len(content)))

	// Basic validation
	if content == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}

	// If filename is empty, generate a default one
	if filename == "" {
		filename = fmt.Sprintf("%s.pdf", title)
		if filename == ".pdf" {
			filename = fmt.Sprintf("document_%d.pdf", time.Now().Unix())
		}
	}

	// Ensure filename has .pdf extension
	if !strings.HasSuffix(filename, ".pdf") {
		filename += ".pdf"
	}

	// Create output directory if it doesn't exist
	outputDir := "/tmp/pdfs"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Full file path
	filePath := filepath.Join(outputDir, filename)

	// Add Noto Sans SC font for Chinese support
	fontPath := findNotoFont()
	var fontFamily string
	var pdf *gofpdf.Fpdf

	if fontPath != "" {
		// Verify file exists
		if _, err := os.Stat(fontPath); err == nil {
			// Get font directory and filename
			fontDir := filepath.Dir(fontPath)
			fontFileName := filepath.Base(fontPath)

			// Create PDF with font directory set (this avoids path issues)
			pdf = gofpdf.NewCustom(&gofpdf.InitType{
				OrientationStr: "P",
				UnitStr:        "mm",
				SizeStr:        "A4",
				FontDirStr:     filepath.ToSlash(fontDir),
			})
			pdf.AddPage()

			// Add font using filename only (directory is already set)
			logger.Base().Debug("Adding font: from directory", zap.String("fontdir", fontDir), zap.String("fontfilename", fontFileName))
			pdf.AddUTF8Font("NotoSansSC", "", fontFileName)
			fontFamily = "NotoSansSC"
			logger.Base().Info("Added Noto Sans SC font successfully")
		} else {
			logger.Base().Warn("Font file not accessible: , using Times")
			fontFamily = "Times"
			pdf = gofpdf.New("P", "mm", "A4", "")
			pdf.AddPage()
		}
	} else {
		fontFamily = "Times"
		logger.Base().Warn("Noto font not found, using Times (Chinese may not display correctly)")
		pdf = gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()
	}

	// Set title font (use regular style, not bold, to avoid font errors)
	pdf.SetFont(fontFamily, "", 16)
	pdf.CellFormat(40, 10, title, "", 0, "", false, 0, "")
	pdf.Ln(12)

	// Set content font
	pdf.SetFont(fontFamily, "", 12)

	// Add content using MultiCell for proper text wrapping with Unicode
	pdf.MultiCell(0, 8, content, "", "", false)

	// Add footer with timestamp
	pdf.SetFont("Arial", "I", 8)
	pdf.SetY(-15)
	pdf.SetX(0)
	pdf.CellFormat(0, 10, fmt.Sprintf("Generated on %s", time.Now().Format("2006-01-02 15:04:05")), "", 0, "C", false, 0, "")

	// Save PDF to file
	if err := pdf.OutputFileAndClose(filePath); err != nil {
		// If error might be font-related and we're using Noto font, retry without font
		errMsg := strings.ToLower(err.Error())
		if fontFamily == "NotoSansSC" && (strings.Contains(errMsg, "font") || strings.Contains(errMsg, "notosans") || strings.Contains(errMsg, "stat")) {
			logger.Base().Error("Font-related error detected: , retrying without custom font")
			// Retry with Times font
			pdf2 := gofpdf.New("P", "mm", "A4", "")
			pdf2.AddPage()
			pdf2.SetFont("Times", "B", 16)
			pdf2.CellFormat(40, 10, title, "", 0, "", false, 0, "")
			pdf2.Ln(12)
			pdf2.SetFont("Times", "", 12)
			pdf2.MultiCell(0, 8, content, "", "", false)
			pdf2.SetFont("Arial", "I", 8)
			pdf2.SetY(-15)
			pdf2.SetX(0)
			pdf2.CellFormat(0, 10, fmt.Sprintf("Generated on %s", time.Now().Format("2006-01-02 15:04:05")), "", 0, "C", false, 0, "")
			if err2 := pdf2.OutputFileAndClose(filePath); err2 != nil {
				return nil, fmt.Errorf("failed to save PDF (with fallback): %w", err2)
			}
			logger.Base().Info("PDF generated successfully with fallback font")
		} else {
			return nil, fmt.Errorf("failed to save PDF: %w", err)
		}
	}

	logger.Base().Info("PDF generated successfully", zap.String("filepath", filePath))

	// Return metadata
	metadata := map[string]interface{}{
		"success":      true,
		"filename":     filename,
		"filepath":     filePath,
		"title":        title,
		"size_bytes":   getFileSize(filePath),
		"generated_at": time.Now().Format(time.RFC3339),
	}

	return metadata, nil
}

// wrapText wraps text to fit within specified width (in mm)
func wrapText(text string, widthMM float64) []string {
	// Simple wrapping: split by sentences and words
	sentences := strings.Split(text, ". ")
	var lines []string
	currentLine := ""

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// Add period if sentence doesn't end with punctuation
		if !strings.HasSuffix(sentence, ".") && !strings.HasSuffix(sentence, "!") && !strings.HasSuffix(sentence, "?") {
			sentence += "."
		}

		// Estimate character width (rough approximation)
		// Average character width in Arial 12pt is about 2.7mm
		charWidth := 2.7
		maxChars := int(widthMM / charWidth)

		if currentLine == "" {
			currentLine = sentence
		} else {
			testLine := currentLine + " " + sentence
			if len(testLine) > maxChars {
				lines = append(lines, currentLine)
				currentLine = sentence
			} else {
				currentLine = testLine
			}
		}

		// If current line is too long, split by words
		if len(currentLine) > maxChars {
			words := strings.Fields(currentLine)
			currentLine = ""
			for _, word := range words {
				if currentLine == "" {
					currentLine = word
				} else {
					testLine := currentLine + " " + word
					if len(testLine) > maxChars {
						lines = append(lines, currentLine)
						currentLine = word
					} else {
						currentLine = testLine
					}
				}
			}
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// getFileSize returns the size of a file in bytes
func getFileSize(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

// UploadPDFToGCS uploads a PDF file to Google Cloud Storage and returns the public URL
func UploadPDFToGCS(ctx context.Context, localFilePath, bucketName, objectPath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(localFilePath); err != nil {
		return "", fmt.Errorf("PDF file not found: %w", err)
	}

	// Create GCS client
	gcsClient, err := gcs.NewGCSClient(ctx, bucketName)
	if err != nil {
		return "", fmt.Errorf("failed to create GCS client: %w", err)
	}
	defer gcsClient.Close()

	// Open the local file
	file, err := os.Open(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer file.Close()

	// Upload to GCS
	url, err := gcsClient.Upload(ctx, objectPath, file)
	if err != nil {
		return "", fmt.Errorf("failed to upload PDF to GCS: %w", err)
	}

	logger.Base().Info("Uploaded to GCS", zap.String("url", url))
	return url, nil
}

// GenerateAndUploadPDFToGCS generates a PDF from text and uploads it to GCS
func GenerateAndUploadPDFToGCS(ctx context.Context, title, content, filename, bucketName string) (map[string]interface{}, error) {
	logger.Base().Info("Generating and uploading PDF to GCS: title=, filename=, bucket=", zap.String("title", title), zap.String("bucketname", bucketName), zap.String("filename", filename))

	// First generate the local PDF
	metadata, err := GeneratePDFFromText(title, content, filename)
	if err != nil {
		return nil, err
	}

	// Get the local file path
	filepath, ok := metadata["filepath"].(string)
	if !ok || filepath == "" {
		return metadata, fmt.Errorf("failed to get local PDF filepath")
	}

	// Use defer to ensure local file is cleaned up regardless of upload success or failure
	defer func() {
		if err := os.Remove(filepath); err != nil {
			logger.Base().Error("Failed to delete local PDF file", zap.String("filepath", filepath))
		} else {
			logger.Base().Info("🗑 Deleted local PDF file", zap.String("filepath", filepath))
		}
	}()

	// Generate object path for GCS
	objectPath := fmt.Sprintf("pdf_message/%s", filename)

	// Upload to GCS
	gcsURL, err := UploadPDFToGCS(ctx, filepath, bucketName, objectPath)
	if err != nil {
		logger.Base().Error("Failed to upload to GCS")
		// Return local metadata even if GCS upload fails
		// Note: defer will clean up the local file automatically
		return metadata, err
	}

	// Add GCS URL to metadata
	metadata["gcs_url"] = gcsURL
	metadata["gcs_bucket"] = bucketName
	metadata["public_url"] = gcsURL

	logger.Base().Info("PDF generated and uploaded successfully", zap.String("gcsurl", gcsURL))
	return metadata, nil
}

// GeneratePDFToWriter writes PDF content to an io.Writer (useful for direct GCS upload)
func GeneratePDFToWriter(title, content string, writer io.Writer) error {
	// Create PDF document
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	// Add Noto Sans SC font for Chinese support
	fontPath := findNotoFont()
	var fontFamily string
	if fontPath != "" {
		// Ensure path is absolute and file exists
		if !filepath.IsAbs(fontPath) {
			if absPath, err := filepath.Abs(fontPath); err == nil {
				fontPath = absPath
			}
		}

		// Verify file exists before using and ensure absolute path with leading /
		if _, err := os.Stat(fontPath); err != nil {
			logger.Base().Warn("Font file not accessible: , using Times")
			fontFamily = "Times"
		} else {
			// Ensure path is absolute and starts with /
			if !strings.HasPrefix(fontPath, "/") {
				fontPath = "/" + fontPath
				logger.Base().Debug("Fixed font path to start with /", zap.String("fontpath", fontPath))
			}
			// Use forward slashes for gofpdf
			fontPathForPDF := filepath.ToSlash(fontPath)
			// Double-check it still starts with /
			if !strings.HasPrefix(fontPathForPDF, "/") {
				fontPathForPDF = "/" + fontPathForPDF
			}
			logger.Base().Debug("Attempting to add font from path", zap.String("fontpathforpdf", fontPathForPDF))
			pdf.AddUTF8Font("NotoSansSC", "", fontPathForPDF)
			fontFamily = "NotoSansSC"
			logger.Base().Info("Added Noto Sans SC font successfully")
		}
	} else {
		fontFamily = "Times"
		logger.Base().Warn("Noto font not found, using Times (Chinese may not display correctly)")
	}

	// Set title font (use regular style, not bold, to avoid font errors)
	pdf.SetFont(fontFamily, "", 16)
	pdf.CellFormat(40, 10, title, "", 0, "", false, 0, "")
	pdf.Ln(12)

	// Set content font
	pdf.SetFont(fontFamily, "", 12)

	// Add content using MultiCell for proper text wrapping with Unicode
	pdf.MultiCell(0, 8, content, "", "", false)

	// Add footer with timestamp
	pdf.SetFont("Arial", "I", 8)
	pdf.SetY(-15)
	pdf.SetX(0)
	pdf.CellFormat(0, 10, fmt.Sprintf("Generated on %s", time.Now().Format("2006-01-02 15:04:05")), "", 0, "C", false, 0, "")

	// Write to writer
	if err := pdf.Output(writer); err != nil {
		return fmt.Errorf("failed to write PDF: %w", err)
	}

	return nil
}
