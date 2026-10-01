package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/e2ee"
	"pigcloud/internal/output"

	"github.com/spf13/cobra"
)

var ulCmd = &cobra.Command{
	Use:     "ul <local-path> [remote-path]",
	GroupID: GroupFiles,
	Aliases: []string{"upload"},
	Short:   "Upload a file or directory to cloud storage",
	Example: `pc ul report.pdf /Documents/     # Upload a file
  pc ul ./my-folder /Backups/      # Upload a directory recursively
  echo "hello" | pc ul - /hello.txt  # Upload from stdin`,
	Long: `Upload a local file or directory to your cloud storage.

If remote-path is not specified, uploads to the current working directory.
If remote-path is a directory, the file keeps its original name.
If a directory is given, all files are uploaded recursively.

Use '-' as the local path to read from stdin. Remote path is required
when uploading from stdin.`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		localPath := args[0]
		remotePath := ""
		if len(args) > 1 {
			remotePath = args[1]
		}
		runUpload(localPath, remotePath)
	},
}

var (
	ulSkipExisting       bool
	ulForce              bool
	ulJobs               int
	ulPreserveTimestamps bool
)

func init() {
	rootCmd.AddCommand(ulCmd)
	ulCmd.Flags().BoolVar(&ulSkipExisting, "skip-existing", false, "skip files that already exist on the remote")
	ulCmd.Flags().BoolVarP(&ulForce, "force", "f", false, "on name collision create a sibling instead of versioning")
	ulCmd.Flags().IntVarP(&ulJobs, "jobs", "j", 1, "number of parallel uploads for directory uploads")
	ulCmd.Flags().BoolVar(&ulPreserveTimestamps, "preserve-timestamps", false, "forward the file's modified time and (for JPEGs) the EXIF capture date so the gallery groups by the photo's real-life date")
}

func runUpload(localPath, remotePath string) {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()

	stdinMode := localPath == "-"
	if stdinMode {
		if remotePath == "" {
			output.PrintError("Remote path is required when uploading from stdin")
			ExitWithError()
		}
		tmpFile, err := os.CreateTemp("", "pigcloud-stdin-*")
		if err != nil {
			output.PrintError("Failed to create temp file: " + err.Error())
			ExitWithError()
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		if _, err := io.Copy(tmpFile, os.Stdin); err != nil {
			tmpFile.Close()
			output.PrintError("Failed to read stdin: " + err.Error())
			ExitWithError()
		}
		tmpFile.Close()
		localPath = tmpPath
	}

	stat, err := os.Stat(localPath)
	if err != nil {
		output.PrintError("Cannot access file: " + err.Error())
		ExitWithError()
	}

	if stat.IsDir() {
		runRecursiveUpload(ctx, localPath, remotePath)
		return
	}

	resolvedPath := cmdutil.ResolvePath(remotePath)
	fileName := filepath.Base(localPath)
	if stdinMode {
		if strings.HasSuffix(resolvedPath, "/") {
			output.PrintError("Stdin upload needs a full remote file path, e.g. pc ul - /notes/log.txt")
			ExitWithError()
		}
		fileName = path.Base(resolvedPath)
		resolvedPath = path.Dir(resolvedPath)
		if resolvedPath != "/" {
			resolvedPath += "/"
		}
	}

	if ulSkipExisting {
		remoteCheck := resolvedPath
		if strings.HasSuffix(remoteCheck, "/") || remoteCheck == "/" {
			remoteCheck = remoteCheck + fileName
		}
		inOpts := map[string]string{"source": remoteCheck}
		e2ee.AddPathTokensFor(inOpts, remoteCheck, e2ee.SelfAndParent, ExitWithError)
		client := api.NewClient()
		resp, _ := client.Execute(ctx, "in", inOpts)
		if resp != nil && resp.Success {
			if !GetQuietOutput() {
				output.PrintInfo("Skipping (exists): " + remoteCheck)
			}
			return
		}
	}

	uploadPath := localPath
	fileSize := stat.Size()
	plainSize := stat.Size()

	fullUploadPath := resolvedPath
	if strings.HasSuffix(fullUploadPath, "/") {
		fullUploadPath += fileName
	} else {
		fullUploadPath += "/" + fileName
	}

	var e2eeOpts map[string]string
	if e2ee.HasE2EEKeys() {
		encPath, sealedKey, encMeta, teeSealedKey, plaintextHmac := e2ee.HandleE2EEUpload(localPath, ExitWithError)
		defer os.Remove(encPath)
		uploadPath = encPath
		e2eeOpts = map[string]string{
			"sealed_key":      sealedKey,
			"encryption_meta": encMeta,
			"_original_name":  fileName,
		}
		applyPreserveTimestamps(e2eeOpts, stat, localPath)
		if teeSealedKey != "" {
			e2eeOpts["tee_sealed_key"] = teeSealedKey
		}
		if plaintextHmac != "" {
			e2eeOpts["plaintext_hmac"] = plaintextHmac
		}
		if ulForce {
			e2eeOpts["force"] = "true"
		}
		sigEd, sigMl, pkEd, pkMl := e2ee.SignEncryptedFile(encPath, ExitWithError)
		e2eeOpts["signature_ed25519"] = sigEd
		e2eeOpts["signature_mldsa"] = sigMl
		e2eeOpts["signing_pk_ed25519"] = pkEd
		e2eeOpts["signing_pk_mldsa"] = pkMl
		uploadFullPath := strings.TrimLeft(fullUploadPath, "/")
		e2ee.AddE2eeNameFields(e2eeOpts, fileName, uploadFullPath, ExitWithError)

		e2ee.AddPathTokensFor(e2eeOpts, resolvedPath, e2ee.SelfAndParent, ExitWithError)

		if encStat, err := os.Stat(encPath); err == nil {
			fileSize = encStat.Size()
		}
	}

	client := api.NewClient()
	if forceCollisionBlocked(ctx, client, fullUploadPath, fileSize) {
		output.PrintError(fullUploadPath + " already exists. " + forceCollisionHint)
		ExitWithError()
	}

	bar := output.NewProgressBar(fileSize, "Uploading "+fileName)

	resp, err := client.Upload(ctx, uploadPath, resolvedPath, func(sent, total int64) {
		bar.Set64(sent)
	}, e2eeOpts)

	bar.Finish()

	if err != nil {
		output.PrintError("Upload failed: " + err.Error())
		ExitWithError()
	}

	if !resp.Success {
		output.PrintError(resp.Message)
		ExitWithError()
	}

	var payload api.UploadPayload
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		output.PrintError("Failed to parse response: " + err.Error())
		ExitWithError()
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if payload.NodeID != "" {
		e2ee.PropagateNameToShares(ctx, payload.NodeID, fileName)
	}

	if !GetQuietOutput() {
		output.PrintSuccess("Uploaded " + payload.Name + " to " + output.PrintPath(payload.StoredPath))
		fmt.Printf("  Size: %s\n", output.FormatSize(&plainSize))
		fmt.Printf("  Storage: %s / %s\n", output.FormatSize(&payload.Storage.UsedBytes), output.FormatSize(&payload.Storage.LimitBytes))
	}
}

func runRecursiveUpload(ctx context.Context, localDir, remotePath string) {
	resolvedRemote := cmdutil.ResolvePath(remotePath)
	dirName := filepath.Base(localDir)

	remoteRoot := resolvedRemote + "/" + dirName
	if strings.HasSuffix(resolvedRemote, "/") {
		remoteRoot = resolvedRemote + dirName
	}

	type fileEntry struct {
		localPath string
		relPath   string
		size      int64
	}

	var files []fileEntry
	var totalSize int64

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel := relativePath(localDir, path)
			files = append(files, fileEntry{
				localPath: path,
				relPath:   rel,
				size:      info.Size(),
			})
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		output.PrintError("Failed to scan directory: " + err.Error())
		ExitWithError()
	}

	if len(files) == 0 {
		output.PrintWarning("Directory is empty, nothing to upload.")
		return
	}

	fmt.Printf("Uploading %d files (%s) from %s to %s\n",
		len(files), output.FormatSize(&totalSize), dirName, remoteRoot)

	dirs := collectDirs(localDir)
	client := api.NewClient()

	if err := ensureRemoteDir(ctx, client, remoteRoot); err != nil {
		output.PrintError("Failed to create remote directory: " + err.Error())
		ExitWithError()
	}
	for _, dir := range dirs {
		remoteDirPath := remoteRoot + "/" + filepath.ToSlash(dir)
		if err := ensureRemoteDir(ctx, client, remoteDirPath); err != nil {
			output.PrintWarning("Failed to create directory " + remoteDirPath + ": " + err.Error())
		}
	}

	var succeeded, failed, skipped int64
	useE2EE := e2ee.HasE2EEKeys()

	uploadOne := func(i int, f fileEntry) {
		remoteFilePath := remoteRoot + "/" + filepath.ToSlash(f.relPath)

		if ulSkipExisting {
			skipOpts := map[string]string{"source": remoteFilePath}
			if useE2EE {
				e2ee.AddPathTokensFor(skipOpts, remoteFilePath, e2ee.SelfAndParent, ExitWithError)
			}
			resp, _ := client.Execute(ctx, "in", skipOpts)
			if resp != nil && resp.Success {
				atomic.AddInt64(&skipped, 1)
				return
			}
		}

		if ctx.Err() != nil {
			atomic.AddInt64(&failed, 1)
			return
		}

		label := fmt.Sprintf("[%d/%d] %s", i+1, len(files), filepath.ToSlash(f.relPath))
		uploadPath := f.localPath
		uploadSize := f.size
		fileName := filepath.Base(f.localPath)
		var e2eeOpts map[string]string
		var tempEncrypted string

		if useE2EE {
			encPath, sealedKey, encMeta, teeSealedKey, plaintextHmac := e2ee.HandleE2EEUpload(f.localPath, ExitWithError)
			tempEncrypted = encPath
			uploadPath = encPath
			e2eeOpts = map[string]string{
				"sealed_key":      sealedKey,
				"encryption_meta": encMeta,
				"_original_name":  fileName,
			}
			if fileStat, statErr := os.Stat(f.localPath); statErr == nil {
				applyPreserveTimestamps(e2eeOpts, fileStat, f.localPath)
			}
			if teeSealedKey != "" {
				e2eeOpts["tee_sealed_key"] = teeSealedKey
			}
			if plaintextHmac != "" {
				e2eeOpts["plaintext_hmac"] = plaintextHmac
			}
			if ulForce {
				e2eeOpts["force"] = "true"
			}
			sigEd, sigMl, pkEd, pkMl := e2ee.SignEncryptedFile(encPath, ExitWithError)
			e2eeOpts["signature_ed25519"] = sigEd
			e2eeOpts["signature_mldsa"] = sigMl
			e2eeOpts["signing_pk_ed25519"] = pkEd
			e2eeOpts["signing_pk_mldsa"] = pkMl

			fullUploadPath := strings.TrimLeft(remoteFilePath, "/")
			e2ee.AddE2eeNameFields(e2eeOpts, fileName, fullUploadPath, ExitWithError)

			if parentDir := path.Dir(fullUploadPath); parentDir != "." && parentDir != "" {
				e2ee.AddPathTokensFor(e2eeOpts, parentDir, e2ee.SelfAndAncestors, ExitWithError)
			}

			if encStat, err := os.Stat(encPath); err == nil {
				uploadSize = encStat.Size()
			}
		}

		if forceCollisionBlocked(ctx, client, remoteFilePath, uploadSize) {
			if tempEncrypted != "" {
				os.Remove(tempEncrypted)
			}
			output.PrintError("Skipping " + f.relPath + ": already exists. " + forceCollisionHint)
			atomic.AddInt64(&failed, 1)
			return
		}

		bar := output.NewProgressBar(uploadSize, label)

		resp, err := client.Upload(ctx, uploadPath, remoteParentDir(remoteFilePath), func(sent, total int64) {
			bar.Set64(sent)
		}, e2eeOpts)

		bar.Finish()

		if tempEncrypted != "" {
			os.Remove(tempEncrypted)
		}

		if err != nil {
			output.PrintError("Failed to upload " + f.relPath + ": " + err.Error())
			atomic.AddInt64(&failed, 1)
			return
		}

		if !resp.Success {
			output.PrintError("Failed to upload " + f.relPath + ": " + resp.Message)
			atomic.AddInt64(&failed, 1)
			return
		}

		if useE2EE {
			var payload api.UploadPayload
			if err := json.Unmarshal(resp.Raw, &payload); err == nil && payload.NodeID != "" {
				e2ee.PropagateNameToShares(ctx, payload.NodeID, fileName)
			}
		}

		atomic.AddInt64(&succeeded, 1)
	}

	jobs := ulJobs
	if jobs < 1 {
		jobs = 1
	}
	if jobs > len(files) {
		jobs = len(files)
	}

	if jobs <= 1 {
		for i, f := range files {
			uploadOne(i, f)
		}
	} else {
		var wg sync.WaitGroup
		ch := make(chan int, len(files))
		for i := range files {
			ch <- i
		}
		close(ch)

		for w := 0; w < jobs; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range ch {
					uploadOne(i, files[i])
				}
			}()
		}
		wg.Wait()
	}

	s, f, sk := atomic.LoadInt64(&succeeded), atomic.LoadInt64(&failed), atomic.LoadInt64(&skipped)
	if !GetQuietOutput() {
		fmt.Println()
		msg := fmt.Sprintf("Uploaded %d files to %s", s, remoteRoot)
		if sk > 0 {
			msg += fmt.Sprintf(", %d skipped", sk)
		}
		if f > 0 {
			output.PrintWarning(msg + fmt.Sprintf(", %d failed", f))
		} else {
			output.PrintSuccess(msg)
		}
	}
}

func ensureRemoteDir(ctx context.Context, client *api.Client, remotePath string) error {
	options := map[string]string{
		"source":  remotePath,
		"parents": "true",
	}
	if e2ee.HasE2EEKeys() {
		e2ee.AddPathTokensFor(options, remotePath, e2ee.SelfAndParent, ExitWithError)
		if trimmed := strings.TrimPrefix(remotePath, "/"); trimmed != "" {
			e2ee.AddE2eeNameFieldsForMkParents(options, strings.Split(trimmed, "/"), ExitWithError)
		}
	}
	_, err := client.Execute(ctx, "mk", options)
	return err
}

func forceCollisionBlocked(ctx context.Context, client *api.Client, remoteFilePath string, ciphertextSize int64) bool {
	if !ulForce || !api.UploadIsChunked(ciphertextSize) {
		return false
	}
	opts := map[string]string{"source": remoteFilePath}
	e2ee.AddPathTokensFor(opts, remoteFilePath, e2ee.SelfAndParent, ExitWithError)
	resp, err := client.Execute(ctx, "in", opts)
	return err == nil && resp != nil && resp.Success
}

const forceCollisionHint = "--force cannot create a same-name copy for files this large. " +
	"Re-run without --force to store the upload as a new version, or rename the file first."

func remoteParentDir(remoteFilePath string) string {
	dir := path.Dir(remoteFilePath)
	if dir == "." || dir == "" {
		return "/"
	}
	return dir
}

func collectDirs(root string) []string {
	var dirs []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && path != root {
			rel := relativePath(root, path)
			dirs = append(dirs, rel)
		}
		return nil
	})
	return dirs
}

func relativePath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}

func applyPreserveTimestamps(opts map[string]string, stat os.FileInfo, localPath string) {
	if !ulPreserveTimestamps || opts == nil {
		return
	}
	if mt := stat.ModTime().Unix(); mt > 0 {
		opts["source_mtime"] = fmt.Sprintf("%d", mt)
	}
	if captured := cmdutil.ParseExifCaptureDate(localPath); captured > 0 {
		opts["captured_at"] = fmt.Sprintf("%d", captured)
	}
}
