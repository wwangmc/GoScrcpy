package main

import "errors"

// Web diagnostics use the same methods and serial checks as native bindings.
// Local paths never come from HTTP: uploads/downloads use native OS dialogs.
func (a *App) fileRequest(method string, args []string) (any, error) {
	counts := map[string]int{"OpenFileManager": 1, "BrowseFiles": 3, "FileSharedApps": 1, "CreateFileDirectory": 4, "RenameFile": 4, "DeleteFile": 3, "PasteFile": 6, "UploadFile": 3, "DownloadFile": 3, "GetFileTransfer": 0, "CancelFileTransfer": 0, "GetFileLocation": 1, "RememberDevice": 1}
	count, ok := counts[method]
	if !ok || len(args) != count {
		return nil, errors.New("无效文件操作参数")
	}
	switch method {
	case "GetFileLocation":
		return a.GetFileLocation(args[0])
	case "RememberDevice":
		return nil, a.RememberDevice(args[0])
	case "OpenFileManager":
		return nil, a.OpenFileManager(args[0])
	case "BrowseFiles":
		return a.BrowseFiles(args[0], args[1], args[2])
	case "FileSharedApps":
		return a.FileSharedApps(args[0])
	case "CreateFileDirectory":
		return nil, a.CreateFileDirectory(args[0], args[1], args[2], args[3])
	case "RenameFile":
		return nil, a.RenameFile(args[0], args[1], args[2], args[3])
	case "DeleteFile":
		return nil, a.DeleteFile(args[0], args[1], args[2])
	case "PasteFile":
		return nil, a.PasteFile(args[0], args[1], args[2], args[3], args[4], args[5])
	case "UploadFile":
		return a.UploadFile(args[0], args[1], args[2])
	case "DownloadFile":
		return a.DownloadFile(args[0], args[1], args[2])
	case "GetFileTransfer":
		return a.GetFileTransfer(), nil
	case "CancelFileTransfer":
		a.CancelFileTransfer()
		return nil, nil
	}
	return nil, errors.New("未知文件操作")
}
