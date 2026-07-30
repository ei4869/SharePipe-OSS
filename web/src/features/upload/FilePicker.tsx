import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import {
  ChevronRight,
  FileArchive,
  FileText,
  FileVideo,
  Folder,
  FolderOpen,
  Info,
  Upload,
  X,
} from "lucide-react";
import { zhCN } from "../../i18n/zh-CN";
import { basename, formatBytes } from "../../utils";
import type { PickedFile } from "./types";
import {
  fileSystemPicker,
  readDirectoryHandle,
  type PersistentFileHandle,
} from "./file-system-access";
import { buildUploadTree, flattenUploadTree } from "./upload-tree";

interface LegacyEntry {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
}

interface LegacyFileEntry extends LegacyEntry {
  file: (callback: (file: File) => void, error?: (problem: DOMException) => void) => void;
}

interface LegacyDirectoryEntry extends LegacyEntry {
  createReader: () => {
    readEntries: (
      callback: (entries: LegacyEntry[]) => void,
      error?: (problem: DOMException) => void,
    ) => void;
  };
}

interface FilePickerProps {
  files: PickedFile[];
  disabled: boolean;
  onAdd: (files: Array<{ file: File; path: string; handle?: PersistentFileHandle }>) => void;
  onRemove: (id: string) => void;
}

export function FilePicker({ files, disabled, onAdd, onRemove }: FilePickerProps) {
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [collapsedPaths, setCollapsedPaths] = useState<Set<string>>(() => new Set());
  const uploadTree = useMemo(() => buildUploadTree(files), [files]);
  const visibleNodes = useMemo(
    () => flattenUploadTree(uploadTree, collapsedPaths),
    [uploadTree, collapsedPaths],
  );

  useEffect(() => {
    folderInput.current?.setAttribute("webkitdirectory", "");
    folderInput.current?.setAttribute("directory", "");
  }, []);

  const addInputFiles = (list: FileList | null) => {
    if (!list) return;
    onAdd(
      Array.from(list).map((file) => ({
        file,
        path: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name,
      })),
    );
  };

  const chooseFiles = async () => {
    const picker = fileSystemPicker();
    if (!picker.showOpenFilePicker) {
      fileInput.current?.click();
      return;
    }
    try {
      const handles = await picker.showOpenFilePicker({ multiple: true });
      onAdd(await Promise.all(handles.map(async (handle) => ({ file: await handle.getFile(), path: handle.name, handle }))));
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      fileInput.current?.click();
    }
  };

  const chooseFolder = async () => {
    const picker = fileSystemPicker();
    if (!picker.showDirectoryPicker) {
      folderInput.current?.click();
      return;
    }
    try {
      onAdd(await readDirectoryHandle(await picker.showDirectoryPicker()));
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      folderInput.current?.click();
    }
  };

  const handleDrop = async (event: React.DragEvent) => {
    event.preventDefault();
    setDragging(false);
    if (disabled) return;
    const entries: LegacyEntry[] = Array.from(event.dataTransfer.items)
      .map((item) => {
        const legacy = item as unknown as {
          webkitGetAsEntry?: () => unknown;
        };
        return (legacy.webkitGetAsEntry?.() as LegacyEntry | null | undefined) ?? null;
      })
      .filter((entry): entry is LegacyEntry => entry !== null);
    if (entries.length === 0) {
      addInputFiles(event.dataTransfer.files);
      return;
    }
    const resolved = (
      await Promise.all(entries.map((entry) => readLegacyEntry(entry, "")))
    ).flat();
    onAdd(resolved);
  };

  return (
    <section className="file-picker-section" aria-labelledby="pending-heading">
      <div
        className={"drop-zone" + (dragging ? " is-dragging" : "")}
        onDragEnter={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragOver={(event) => event.preventDefault()}
        onDragLeave={(event) => {
          if (event.currentTarget === event.target) setDragging(false);
        }}
        onDrop={handleDrop}
      >
        <Upload size={42} strokeWidth={1.5} aria-hidden="true" />
        <p>{zhCN.dropHint}</p>
        <div className="drop-actions">
          <button
            className="secondary-button"
            type="button"
            disabled={disabled}
            onClick={() => void chooseFiles()}
          >
            <FileText size={18} strokeWidth={1.75} />
            {zhCN.chooseFile}
          </button>
          <button
            className="secondary-button"
            type="button"
            disabled={disabled}
            onClick={() => void chooseFolder()}
          >
            <FolderOpen size={19} strokeWidth={1.75} />
            {zhCN.chooseFolder}
          </button>
        </div>
        <input
          ref={fileInput}
          className="sr-only"
          type="file"
          multiple
          onChange={(event) => {
            addInputFiles(event.target.files);
            event.target.value = "";
          }}
        />
        <input
          ref={folderInput}
          className="sr-only"
          type="file"
          multiple
          onChange={(event) => {
            addInputFiles(event.target.files);
            event.target.value = "";
          }}
        />
      </div>

      <div className="mobile-picker-actions">
        <button
          className="mobile-picker-button"
          type="button"
          disabled={disabled}
          onClick={() => void chooseFiles()}
        >
          <FileText size={28} strokeWidth={1.6} />
          {zhCN.chooseFile}
        </button>
        <button
          className="mobile-picker-button"
          type="button"
          disabled={disabled}
          onClick={() => void chooseFolder()}
        >
          <FolderOpen size={29} strokeWidth={1.6} />
          {zhCN.chooseFolder}
        </button>
        <p>
          <Info size={17} strokeWidth={1.75} />
          {zhCN.mobileFolderHint}
        </p>
      </div>

      <div className="pending-heading-row">
        <h2 id="pending-heading">{zhCN.pendingFiles}</h2>
        <span>{zhCN.common.fileSummary(files.length, formatBytes(files.reduce((sum, item) => sum + item.file.size, 0)))}</span>
      </div>
      {files.length === 0 ? (
        <div className="empty-files">{zhCN.picker.empty}</div>
      ) : (
        <div className="picked-file-list" role="tree" aria-label={zhCN.pendingFiles}>
          {visibleNodes.map(({ node, depth }) => {
            const rowStyle = { "--upload-tree-depth": depth } as CSSProperties;
            if (node.kind === "directory") {
              const collapsed = collapsedPaths.has(node.path);
              return (
                <button
                  className="picked-tree-row picked-directory-row"
                  key={`directory:${node.path}`}
                  type="button"
                  role="treeitem"
                  aria-level={depth + 1}
                  aria-expanded={!collapsed}
                  aria-label={collapsed ? zhCN.picker.expandDirectory(node.name) : zhCN.picker.collapseDirectory(node.name)}
                  title={node.path}
                  style={rowStyle}
                  onClick={() => {
                    setCollapsedPaths((current) => {
                      const next = new Set(current);
                      if (next.has(node.path)) next.delete(node.path);
                      else next.add(node.path);
                      return next;
                    });
                  }}
                >
                  <ChevronRight
                    className={`directory-chevron${collapsed ? "" : " is-expanded"}`}
                    size={17}
                    strokeWidth={1.9}
                    aria-hidden="true"
                  />
                  <Folder className="file-type-icon directory" size={27} strokeWidth={1.7} />
                  <span className="picked-file-name">
                    <strong>{node.name}</strong>
                    <span>{zhCN.common.fileSummary(node.fileCount, formatBytes(node.totalBytes))}</span>
                  </span>
                  <span className="picked-file-size">{formatBytes(node.totalBytes)}</span>
                  <span className="picked-directory-count">{zhCN.common.fileCount(node.fileCount)}</span>
                  <span aria-hidden="true" />
                </button>
              );
            }

            const item = node.item;
            return (
              <div
                className="picked-tree-row picked-file-row"
                key={item.id}
                role="treeitem"
                aria-level={depth + 1}
                title={node.path}
                style={rowStyle}
              >
                <span className="tree-leaf-spacer" aria-hidden="true" />
                <FileTypeIcon file={item.file} />
                <div className="picked-file-name">
                  <strong>{node.name}</strong>
                  <span>{depth === 0 ? formatBytes(item.file.size) : node.path}</span>
                </div>
                <span className="picked-file-size">{formatBytes(item.file.size)}</span>
                <div className="picked-file-status">
                  <span>
                    {item.status === "uploading"
                      ? zhCN.picker.uploading(Math.round(item.progress))
                      : item.status === "completed"
                        ? zhCN.picker.completed
                        : item.status === "error"
                          ? zhCN.picker.failed
                          : zhCN.picker.waiting}
                  </span>
                  <span className="progress-track" aria-hidden="true">
                    <span style={{ width: item.progress + "%" }} />
                  </span>
                </div>
                <button
                  className="remove-file"
                  type="button"
                  aria-label={zhCN.picker.remove(node.name)}
                  disabled={disabled}
                  onClick={() => onRemove(item.id)}
                >
                  <X size={20} strokeWidth={1.7} />
                </button>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function FileTypeIcon({ file }: { file: File }) {
  if (file.type.startsWith("video/") || file.type.startsWith("audio/")) {
    return <FileVideo className="file-type-icon video" size={27} strokeWidth={1.7} />;
  }
  if (/\.(zip|rar|7z|tar|gz)$/i.test(file.name)) {
    return <FileArchive className="file-type-icon archive" size={27} strokeWidth={1.7} />;
  }
  return <FileText className="file-type-icon" size={27} strokeWidth={1.7} />;
}

async function readLegacyEntry(
  entry: LegacyEntry,
  prefix: string,
): Promise<Array<{ file: File; path: string }>> {
  const path = prefix ? prefix + "/" + entry.name : entry.name;
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) =>
      (entry as LegacyFileEntry).file(resolve, reject),
    );
    return [{ file, path }];
  }
  if (!entry.isDirectory) return [];
  const reader = (entry as LegacyDirectoryEntry).createReader();
  const children: LegacyEntry[] = [];
  while (true) {
    const batch = await new Promise<LegacyEntry[]>((resolve, reject) =>
      reader.readEntries(resolve, reject),
    );
    if (batch.length === 0) break;
    children.push(...batch);
  }
  return (await Promise.all(children.map((child) => readLegacyEntry(child, path)))).flat();
}
