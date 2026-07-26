"use strict";

const HISTORY_KEY = "gotexttoepub.history.v1";
const MAX_HISTORY_ITEMS = 30;
const AUTO_COVER_FILE_NAME = "自动生成封面.png";
const ACTIVE_STATUSES = new Set(["uploading", "queued", "converting"]);
const TERMINAL_STATUSES = new Set(["succeeded", "failed", "expired", "evicted"]);
const STAGES = ["uploading", "queued", "converting", "succeeded"];

const statusLabels = {
  uploading: "正在上传",
  queued: "等待中",
  converting: "转换中",
  succeeded: "可以下载",
  failed: "转换失败",
  expired: "已过期",
  evicted: "已清理",
};

const state = {
  currentJobId: "",
  pollTimer: 0,
  pollAttempts: 0,
  coverObjectUrl: "",
  history: loadHistory(),
};

const elements = {
  form: document.getElementById("conversionForm"),
  fileInput: document.getElementById("fileInput"),
  fileDrop: document.getElementById("fileDrop"),
  fileTitle: document.getElementById("fileTitle"),
  fileMeta: document.getElementById("fileMeta"),
  coverFileInput: document.getElementById("coverFileInput"),
  coverFileDrop: document.getElementById("coverFileDrop"),
  coverFileTitle: document.getElementById("coverFileTitle"),
  coverFileMeta: document.getElementById("coverFileMeta"),
  clearCoverFile: document.getElementById("clearCoverFile"),
  coverUrl: document.getElementById("coverUrl"),
  coverUrlField: document.getElementById("coverUrlField"),
  coverPreview: document.getElementById("coverPreview"),
  coverPreviewImage: document.getElementById("coverPreviewImage"),
  coverPreviewMark: document.getElementById("coverPreviewMark"),
  coverSourceTitle: document.getElementById("coverSourceTitle"),
  coverSourceDescription: document.getElementById("coverSourceDescription"),
  formError: document.getElementById("formError"),
  submitButton: document.getElementById("submitButton"),
  capacity: document.getElementById("capacity"),
  capacityText: document.getElementById("capacityText"),
  progressPanel: document.getElementById("progressPanel"),
  progressTitle: document.getElementById("progressTitle"),
  spineProgress: document.getElementById("spineProgress"),
  jobId: document.getElementById("jobId"),
  uploadProgress: document.getElementById("uploadProgress"),
  queueProgress: document.getElementById("queueProgress"),
  convertProgress: document.getElementById("convertProgress"),
  doneProgress: document.getElementById("doneProgress"),
  statusNote: document.getElementById("statusNote"),
  statusSeal: document.getElementById("statusSeal"),
  statusTitle: document.getElementById("statusTitle"),
  statusMessage: document.getElementById("statusMessage"),
  downloadCard: document.getElementById("downloadCard"),
  downloadName: document.getElementById("downloadName"),
  downloadMeta: document.getElementById("downloadMeta"),
  downloadLink: document.getElementById("downloadLink"),
  historyList: document.getElementById("historyList"),
  historyEmpty: document.getElementById("historyEmpty"),
  clearHistory: document.getElementById("clearHistory"),
  historyTemplate: document.getElementById("historyItemTemplate"),
};

elements.form.addEventListener("submit", handleSubmit);
elements.fileInput.addEventListener("change", updateSelectedFile);
elements.coverFileInput.addEventListener("change", updateSelectedCoverFile);
elements.clearCoverFile.addEventListener("click", clearSelectedCoverFile);
elements.coverUrl.addEventListener("input", updateCoverFromUrl);
elements.coverPreviewImage.addEventListener("error", handleCoverPreviewError);
elements.clearHistory.addEventListener("click", clearHistory);
elements.fileDrop.addEventListener("dragover", handleDragOver);
elements.fileDrop.addEventListener("dragleave", handleDragLeave);
elements.fileDrop.addEventListener("drop", handleDrop);
elements.coverFileDrop.addEventListener("dragover", handleCoverDragOver);
elements.coverFileDrop.addEventListener("dragleave", handleCoverDragLeave);
elements.coverFileDrop.addEventListener("drop", handleCoverDrop);

renderHistory();
refreshCoverPresentation();
refreshHistoryStatuses();
loadCapacity();

function handleDragOver(event) {
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "copy";
  }
  elements.fileDrop.classList.add("is-dragging");
}

function handleDragLeave(event) {
  if (!elements.fileDrop.contains(event.relatedTarget)) {
    elements.fileDrop.classList.remove("is-dragging");
  }
}

function handleDrop(event) {
  event.preventDefault();
  elements.fileDrop.classList.remove("is-dragging");
  const files = event.dataTransfer && event.dataTransfer.files;
  if (!files || files.length === 0) {
    return;
  }

  const transfer = new DataTransfer();
  transfer.items.add(files[0]);
  elements.fileInput.files = transfer.files;
  updateSelectedFile();
}

function handleCoverDragOver(event) {
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "copy";
  }
  elements.coverFileDrop.classList.add("is-dragging");
}

function handleCoverDragLeave(event) {
  if (!elements.coverFileDrop.contains(event.relatedTarget)) {
    elements.coverFileDrop.classList.remove("is-dragging");
  }
}

function handleCoverDrop(event) {
  event.preventDefault();
  elements.coverFileDrop.classList.remove("is-dragging");
  const files = event.dataTransfer && event.dataTransfer.files;
  if (!files || files.length === 0) {
    return;
  }

  const transfer = new DataTransfer();
  transfer.items.add(files[0]);
  elements.coverFileInput.files = transfer.files;
  updateSelectedCoverFile();
}

function updateSelectedFile() {
  const file = elements.fileInput.files[0];
  elements.formError.hidden = true;

  if (!file) {
    elements.fileDrop.classList.remove("has-file");
    elements.fileTitle.textContent = "点击选择，或拖放到这里";
    elements.fileMeta.textContent = "TXT 文档";
    refreshCoverPresentation();
    return;
  }

  elements.fileDrop.classList.add("has-file");
  elements.fileTitle.textContent = file.name;
  elements.fileMeta.textContent = `${formatBytes(file.size)} · 已选择`;
  refreshCoverPresentation();
}

function updateSelectedCoverFile() {
  const coverFile = elements.coverFileInput.files[0];
  elements.formError.hidden = true;

  if (!coverFile) {
    refreshCoverPresentation();
    return;
  }

  elements.coverUrl.value = "";
  refreshCoverPresentation();
}

function clearSelectedCoverFile() {
  elements.coverFileInput.value = "";
  refreshCoverPresentation();
  elements.coverUrl.focus();
}

function updateCoverFromUrl() {
  if (elements.coverUrl.value.trim()) {
    elements.coverFileInput.value = "";
  }
  refreshCoverPresentation();
}

function refreshCoverPresentation() {
  const coverFile = elements.coverFileInput.files[0];
  const coverUrl = elements.coverUrl.value.trim();
  const txtFile = elements.fileInput.files[0];

  clearCoverObjectUrl();
  elements.coverFileDrop.classList.toggle("has-file", Boolean(coverFile));
  elements.coverUrl.disabled = Boolean(coverFile);
  elements.coverUrlField.classList.toggle("is-disabled", Boolean(coverFile));
  elements.clearCoverFile.hidden = !coverFile;

  if (coverFile) {
    elements.coverFileTitle.textContent = coverFile.name;
    elements.coverFileMeta.textContent = `${formatBytes(coverFile.size)} · 将优先使用`;
    const objectUrl = URL.createObjectURL(coverFile);
    state.coverObjectUrl = objectUrl;
    showCoverPreview("upload", objectUrl, "使用上传的封面", "上传的 JPEG/PNG 会随文稿一起提交。", "上传的封面预览");
    return;
  }

  elements.coverFileTitle.textContent = "上传一张封面";
  elements.coverFileMeta.textContent = "JPEG 或 PNG";

  if (coverUrl) {
    showCoverPreview("url", "", "使用封面链接", "链接只会交给服务端安全下载并校验，浏览器不会直接访问。", "");
    return;
  }

  if (txtFile) {
    try {
      const title = titleFromFileName(txtFile.name);
      const previewCanvas = createAutoCoverCanvas(title, 360, 480);
      showCoverPreview(
        "auto",
        previewCanvas.toDataURL("image/png"),
        "将自动生成题签封面",
        `将以“${title}”制成 PNG 封面并随文稿上传。`,
        `“${title}”的自动封面预览`
      );
    } catch {
      showCoverPreview("auto", "", "将自动生成题签封面", "提交时会依据 TXT 文件名生成 PNG 封面。", "");
    }
    return;
  }

  showCoverPreview("auto", "", "将自动生成题签封面", "选择 TXT 后，会依据文件名生成 PNG 并随文稿上传。", "");
}

function showCoverPreview(source, imageUrl, title, description, alt) {
  elements.coverPreview.dataset.source = source;
  elements.coverSourceTitle.textContent = title;
  elements.coverSourceDescription.textContent = description;
  elements.coverPreviewImage.dataset.source = source;

  if (imageUrl) {
    elements.coverPreviewImage.src = imageUrl;
    elements.coverPreviewImage.alt = alt;
    elements.coverPreviewImage.hidden = false;
    elements.coverPreviewMark.hidden = true;
    return;
  }

  elements.coverPreviewImage.removeAttribute("src");
  elements.coverPreviewImage.alt = "";
  elements.coverPreviewImage.hidden = true;
  elements.coverPreviewMark.hidden = false;
}

function handleCoverPreviewError() {
  if (elements.coverPreviewImage.dataset.source !== "url") {
    return;
  }
  elements.coverPreviewImage.hidden = true;
  elements.coverPreviewMark.hidden = false;
  elements.coverSourceDescription.textContent = "此链接暂时无法在浏览器中预览；提交后仍会由服务端安全校验。";
}

function clearCoverObjectUrl() {
  if (state.coverObjectUrl) {
    URL.revokeObjectURL(state.coverObjectUrl);
    state.coverObjectUrl = "";
  }
}

async function handleSubmit(event) {
  event.preventDefault();
  clearTimeout(state.pollTimer);
  elements.formError.hidden = true;

  const file = elements.fileInput.files[0];
  const uploadedCover = elements.coverFileInput.files[0];
  const coverUrl = elements.coverUrl.value.trim();
  const validationError = validateForm(file, uploadedCover, coverUrl);
  if (validationError) {
    showFormError(validationError);
    return;
  }

  setSubmitting(true);
  let coverFile = uploadedCover;
  if (!coverFile && !coverUrl) {
    try {
      coverFile = await createAutoCoverFile(file);
    } catch {
      setSubmitting(false);
      showFormError("无法生成默认封面，请刷新页面后重试，或上传一张 JPEG/PNG 图片。");
      return;
    }
  }

  state.currentJobId = "";
  state.pollAttempts = 0;
  showProgressPanel();
  updateProgress("uploading", {
    message: "正在把文稿送往装订台，请不要关闭页面。",
    uploadPercent: 0,
  });

  const formData = new FormData();
  formData.append("file", file, file.name);
  if (coverFile) {
    formData.append("cover_file", coverFile, coverFile.name);
  } else if (coverUrl) {
    formData.append("cover_url", coverUrl);
  }

  const xhr = new XMLHttpRequest();
  xhr.open("POST", "/api/conversions");
  xhr.responseType = "text";
  xhr.timeout = 120000;

  xhr.upload.addEventListener("progress", (progressEvent) => {
    if (!progressEvent.lengthComputable) {
      elements.uploadProgress.textContent = "正在上传";
      return;
    }
    const percent = Math.min(100, Math.round((progressEvent.loaded / progressEvent.total) * 100));
    updateProgress("uploading", {
      message: `文稿已上传 ${percent}%，请保持页面开启。`,
      uploadPercent: percent,
    });
  });

  xhr.addEventListener("load", () => {
    const payload = responsePayload(xhr);
    if (xhr.status < 200 || xhr.status >= 300) {
      handleSubmissionError(payload, xhr.status);
      return;
    }

    const id = stringValue(payload.id || payload.jobId);
    if (!id) {
      handleSubmissionError({ message: "服务端没有返回任务编号。" }, xhr.status);
      return;
    }

    state.currentJobId = id;
    elements.jobId.textContent = shortId(id);
    const status = normalizeStatus(payload.status) || "queued";
    const record = sanitizeRecord({
      id,
      originalName: file.name,
      inputSize: file.size,
      status,
      createdAt: payload.createdAt || new Date().toISOString(),
      completedAt: payload.completedAt,
      expiresAt: payload.expiresAt,
      outputName: payload.outputName,
      outputSize: payload.outputSize,
      errorCode: payload.errorCode,
    });
    upsertHistory(record);
    applyJob(payload, record);
    schedulePoll(id);
    loadCapacity();
  });

  xhr.addEventListener("error", () => {
    handleSubmissionError({ message: "网络连接失败，请检查连接后重试。" }, 0);
  });

  xhr.addEventListener("timeout", () => {
    handleSubmissionError({ message: "上传超时，请检查网络或减小文件后重试。" }, 0);
  });

  xhr.addEventListener("abort", () => {
    handleSubmissionError({ message: "上传已中止。" }, 0);
  });

  xhr.send(formData);
}

function validateForm(file, coverFile, coverUrl) {
  if (!file) {
    return "请先选择一个 TXT 文档。";
  }
  if (!file.name.toLowerCase().endsWith(".txt")) {
    return "请选择扩展名为 .txt 的纯文本文档。";
  }
  if (coverFile && !isSupportedCoverFile(coverFile)) {
    return "封面仅支持 JPEG 或 PNG 图片。";
  }
  if (!coverFile && coverUrl) {
    try {
      const parsedUrl = new URL(coverUrl);
      if (parsedUrl.protocol !== "https:") {
        return "封面链接仅支持 HTTPS。";
      }
    } catch {
      return "封面链接格式不正确，请输入完整网址。";
    }
  }
  return "";
}

function isSupportedCoverFile(file) {
  const lowerName = file.name.toLowerCase();
  return file.type === "image/jpeg" || file.type === "image/png" || /\.(jpe?g|png)$/.test(lowerName);
}

function titleFromFileName(fileName) {
  const rawName = stringValue(fileName).replace(/\.txt$/i, "").trim();
  const normalized = rawName.replace(/[_.-]+/g, " ").replace(/\s+/g, " ").trim();
  const title = normalized || "未命名文稿";
  const characters = Array.from(title);
  return characters.length > 22 ? `${characters.slice(0, 21).join("")}…` : title;
}

function createAutoCoverCanvas(title, width, height) {
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext("2d");
  if (!context) {
    throw new Error("Canvas is unavailable");
  }

  const seed = hashText(title);
  const paper = "#f2ecdf";
  const ink = "#25221d";
  const cinnabar = "#a63b2a";
  const margin = Math.round(width * 0.105);
  const top = Math.round(height * 0.12);
  const titleSize = Math.round(width * 0.105);

  context.fillStyle = ink;
  context.fillRect(0, 0, width, height);

  context.fillStyle = "rgba(230, 193, 142, 0.16)";
  context.fillRect(0, 0, width, Math.round(height * 0.22));
  context.fillStyle = "rgba(166, 59, 42, 0.16)";
  context.fillRect(0, Math.round(height * 0.78), width, Math.round(height * 0.22));

  context.strokeStyle = "rgba(242, 236, 223, 0.42)";
  context.lineWidth = Math.max(2, Math.round(width * 0.005));
  context.strokeRect(margin, margin, width - margin * 2, height - margin * 2);
  context.strokeStyle = "rgba(242, 236, 223, 0.18)";
  context.lineWidth = Math.max(1, Math.round(width * 0.002));
  context.strokeRect(margin + width * 0.024, margin + width * 0.024, width - (margin + width * 0.024) * 2, height - (margin + width * 0.024) * 2);

  const motifX = width * (0.18 + ((seed >>> 3) % 38) / 100);
  const motifY = height * (0.67 + ((seed >>> 9) % 12) / 100);
  const motifRadius = width * (0.22 + ((seed >>> 15) % 9) / 100);
  context.beginPath();
  context.arc(motifX, motifY, motifRadius, Math.PI * 1.05, Math.PI * 1.94);
  context.strokeStyle = "rgba(230, 193, 142, 0.64)";
  context.lineWidth = Math.max(2, Math.round(width * 0.01));
  context.stroke();
  context.beginPath();
  context.arc(width * 0.72, height * 0.6, motifRadius * 0.72, Math.PI * 0.12, Math.PI * 0.94);
  context.strokeStyle = "rgba(166, 59, 42, 0.78)";
  context.stroke();

  context.fillStyle = paper;
  context.textAlign = "center";
  context.textBaseline = "middle";
  context.font = `700 ${titleSize}px "Songti SC", "STSong", serif`;
  const titleLines = coverTitleLines(title);
  const lineHeight = Math.round(titleSize * 1.38);
  const titleStartY = top + (height * 0.2) - ((titleLines.length - 1) * lineHeight) / 2;
  titleLines.forEach((line, index) => {
    context.fillText(line, width / 2, titleStartY + index * lineHeight, width - margin * 2.7);
  });

  context.fillStyle = "rgba(242, 236, 223, 0.7)";
  context.font = `600 ${Math.round(width * 0.026)}px Georgia, serif`;
  context.fillText("TXT · EPUB", width / 2, height * 0.51);

  const sealSize = Math.round(width * 0.12);
  const sealX = width - margin - sealSize;
  const sealY = height - margin - sealSize;
  context.fillStyle = cinnabar;
  context.fillRect(sealX, sealY, sealSize, sealSize);
  context.strokeStyle = "rgba(242, 236, 223, 0.88)";
  context.lineWidth = Math.max(1, Math.round(width * 0.003));
  context.strokeRect(sealX + sealSize * 0.12, sealY + sealSize * 0.12, sealSize * 0.76, sealSize * 0.76);
  context.fillStyle = paper;
  context.font = `700 ${Math.round(sealSize * 0.46)}px "Songti SC", "STSong", serif`;
  context.fillText("拾", sealX + sealSize / 2, sealY + sealSize * 0.54);

  return canvas;
}

function coverTitleLines(title) {
  const characters = Array.from(title);
  if (characters.length <= 8) {
    return [title];
  }
  const midpoint = Math.ceil(characters.length / 2);
  return [characters.slice(0, midpoint).join(""), characters.slice(midpoint).join("")];
}

function hashText(value) {
  let hash = 2166136261;
  for (const character of value) {
    hash ^= character.codePointAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function createAutoCoverFile(txtFile) {
  return new Promise((resolve, reject) => {
    const canvas = createAutoCoverCanvas(titleFromFileName(txtFile.name), 900, 1200);
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error("Could not create cover image"));
        return;
      }
      resolve(new File([blob], AUTO_COVER_FILE_NAME, { type: "image/png" }));
    }, "image/png");
  });
}

function handleSubmissionError(payload, statusCode) {
  setSubmitting(false);
  const message = readableError(payload, statusCode);
  showFormError(message);
  updateProgress("failed", { message, errorCode: payload.errorCode || payload.code });
  loadCapacity();
}

function responsePayload(xhr) {
  if (xhr.response && typeof xhr.response === "object") {
    return xhr.response;
  }
  if (typeof xhr.responseText === "string" && xhr.responseText) {
    try {
      return JSON.parse(xhr.responseText);
    } catch {
      return { message: xhr.responseText };
    }
  }
  return {};
}

async function pollJob(id) {
  if (!id) {
    return;
  }

  try {
    const response = await fetch(`/api/conversions/${encodeURIComponent(id)}`, {
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    const payload = await safeJson(response);
    if (!response.ok) {
      if (response.status === 404 || response.status === 410) {
        const record = findHistory(id);
        if (record) {
          record.status = response.status === 410 ? "expired" : "failed";
          record.errorCode = stringValue(payload.errorCode || payload.code);
          upsertHistory(record);
          if (id === state.currentJobId) {
            applyJob(payload, record);
          }
        }
        return;
      }
      throw new Error(readableError(payload, response.status));
    }

    const existing = findHistory(id) || { id };
    const record = sanitizeRecord({
      ...existing,
      id,
      status: payload.status || existing.status,
      createdAt: payload.createdAt || existing.createdAt,
      completedAt: payload.completedAt || existing.completedAt,
      expiresAt: payload.expiresAt || existing.expiresAt,
      outputName: payload.outputName || payload.fileName || existing.outputName,
      outputSize: payload.outputSize ?? payload.fileSize ?? existing.outputSize,
      errorCode: payload.errorCode || payload.code || existing.errorCode,
    });
    upsertHistory(record);

    if (id === state.currentJobId) {
      applyJob(payload, record);
    }

    if (ACTIVE_STATUSES.has(record.status)) {
      schedulePoll(id);
    } else {
      setSubmitting(false);
      loadCapacity();
    }
  } catch (error) {
    if (id !== state.currentJobId) {
      return;
    }
    state.pollAttempts += 1;
    elements.statusMessage.textContent = "暂时无法获取进度，正在自动重试。";
    if (state.pollAttempts <= 6) {
      schedulePoll(id);
    } else {
      setSubmitting(false);
      elements.statusTitle.textContent = "进度查询中断";
      elements.statusMessage.textContent = error instanceof Error ? error.message : "请刷新页面后从历史记录继续查看。";
      elements.statusNote.classList.add("is-error");
    }
  }
}

function schedulePoll(id) {
  clearTimeout(state.pollTimer);
  const delay = state.pollAttempts >= 5 ? 5000 : 2000;
  state.pollTimer = window.setTimeout(() => pollJob(id), delay);
}

function applyJob(payload, record) {
  const status = normalizeStatus(record.status) || "queued";
  const queuePosition = numberValue(payload.queuePosition ?? payload.position);
  const message = stringValue(payload.message);
  state.pollAttempts = 0;

  updateProgress(status, {
    message,
    queuePosition,
    outputName: record.outputName,
    outputSize: record.outputSize,
    expiresAt: record.expiresAt,
    errorCode: record.errorCode,
  });

  if (TERMINAL_STATUSES.has(status)) {
    setSubmitting(false);
  }
}

function updateProgress(status, details = {}) {
  const normalized = normalizeStatus(status) || "failed";
  const effectiveStage = normalized === "failed" || normalized === "expired" || normalized === "evicted"
    ? terminalStageFromHistory()
    : normalized;
  const stageIndex = Math.max(0, STAGES.indexOf(effectiveStage));

  elements.spineProgress.querySelectorAll("li").forEach((item, index) => {
    item.classList.toggle("is-complete", index < stageIndex || normalized === "succeeded");
    item.classList.toggle("is-active", index === stageIndex && !TERMINAL_STATUSES.has(normalized));
  });
  const progressPercent = (stageIndex / (STAGES.length - 1)) * 75;
  const mobilePercent = (stageIndex / (STAGES.length - 1)) * 100;
  elements.spineProgress.style.setProperty("--progress-width", `${progressPercent}%`);
  elements.spineProgress.style.setProperty("--progress-height", `${mobilePercent}%`);

  elements.statusNote.classList.toggle(
    "is-error",
    normalized === "failed" || normalized === "expired" || normalized === "evicted"
  );
  elements.downloadCard.hidden = normalized !== "succeeded";
  elements.progressTitle.textContent = progressHeading(normalized);

  if (normalized === "uploading") {
    const percent = Number.isFinite(details.uploadPercent) ? details.uploadPercent : 0;
    elements.uploadProgress.textContent = percent > 0 ? `${percent}%` : "正在连接";
    elements.statusSeal.textContent = "传";
    elements.statusTitle.textContent = "正在上传文稿";
    elements.statusMessage.textContent = details.message || "正在把文稿送往装订台。";
  } else if (normalized === "queued") {
    elements.uploadProgress.textContent = "上传完成";
    elements.queueProgress.textContent = details.queuePosition > 0
      ? `前方 ${details.queuePosition} 个任务`
      : "已进入队列";
    elements.statusSeal.textContent = "候";
    elements.statusTitle.textContent = "装订台正在忙";
    elements.statusMessage.textContent = details.message || (
      details.queuePosition > 0
        ? `前方还有 ${details.queuePosition} 个任务，轮到你时会自动开始。`
        : "任务已排队，轮到你时会自动开始。"
    );
  } else if (normalized === "converting") {
    elements.uploadProgress.textContent = "上传完成";
    elements.queueProgress.textContent = "排队完成";
    elements.convertProgress.textContent = "正在生成";
    elements.statusSeal.textContent = "制";
    elements.statusTitle.textContent = "正在生成 EPUB";
    elements.statusMessage.textContent = details.message || "正在整理章节与书页，请稍候。";
  } else if (normalized === "succeeded") {
    elements.uploadProgress.textContent = "上传完成";
    elements.queueProgress.textContent = "排队完成";
    elements.convertProgress.textContent = "生成完成";
    elements.doneProgress.textContent = "可以下载";
    elements.statusSeal.textContent = "成";
    elements.statusTitle.textContent = "电子书装订完成";
    elements.statusMessage.textContent = details.message || "成品已经就绪，请在失效前下载保存。";
    showDownload(details);
  } else {
    elements.statusSeal.textContent = normalized === "failed" ? "误" : "止";
    elements.statusTitle.textContent = statusLabels[normalized] || "任务已停止";
    elements.statusMessage.textContent = details.message || terminalMessage(normalized, details.errorCode);
  }
}

function showDownload(details) {
  const name = stringValue(details.outputName) || "电子书.epub";
  elements.downloadName.textContent = name;
  elements.downloadMeta.textContent = expiryText(details.expiresAt, details.outputSize);
  elements.downloadLink.href = `/api/conversions/${encodeURIComponent(state.currentJobId)}/download`;
  elements.downloadLink.setAttribute("download", name);
}

function showProgressPanel() {
  elements.progressPanel.hidden = false;
  elements.jobId.textContent = "待分配";
  elements.downloadCard.hidden = true;
  resetStageText();
  window.requestAnimationFrame(() => {
    elements.progressPanel.scrollIntoView({ behavior: "smooth", block: "start" });
  });
}

function resetStageText() {
  elements.uploadProgress.textContent = "等待开始";
  elements.queueProgress.textContent = "尚未排队";
  elements.convertProgress.textContent = "尚未开始";
  elements.doneProgress.textContent = "等待成品";
}

function setSubmitting(isSubmitting) {
  elements.submitButton.disabled = isSubmitting;
  const label = elements.submitButton.querySelector("span");
  label.textContent = isSubmitting ? "正在制作，请稍候" : "开始制作 EPUB";
}

function showFormError(message) {
  elements.formError.textContent = message;
  elements.formError.hidden = false;
}

async function loadCapacity() {
  try {
    const response = await fetch("/api/capacity", {
      cache: "no-store",
      credentials: "same-origin",
      headers: { Accept: "application/json" },
    });
    if (!response.ok) {
      throw new Error("capacity unavailable");
    }
    const capacity = await safeJson(response);
    const active = numberValue(capacity.active ?? capacity.running ?? capacity.inUse);
    const limit = numberValue(capacity.limit ?? capacity.maxConcurrent ?? capacity.total);
    const queued = numberValue(capacity.queued ?? capacity.queueSize);
    const available = typeof capacity.available === "boolean"
      ? capacity.available
      : limit <= 0 || active < limit;

    elements.capacity.classList.remove("is-unknown");
    elements.capacity.classList.toggle("is-busy", !available || queued > 0);
    if (limit > 0) {
      elements.capacityText.textContent = available
        ? `装订台空闲 ${Math.max(0, limit - active)} / ${limit}`
        : `装订台繁忙${queued > 0 ? ` · 等待 ${queued}` : ""}`;
    } else {
      elements.capacityText.textContent = available ? "装订台可以使用" : "装订台暂时繁忙";
    }
  } catch {
    elements.capacity.classList.add("is-unknown");
    elements.capacity.classList.remove("is-busy");
    elements.capacityText.textContent = "装订台状态暂不可用";
  }
}

async function refreshHistoryStatuses() {
  const refreshableRecords = state.history.filter(
    (item) => ACTIVE_STATUSES.has(item.status) || item.status === "succeeded"
  );
  if (refreshableRecords.length === 0) {
    return;
  }

  await Promise.allSettled(refreshableRecords.map(async (record) => {
    const response = await fetch(`/api/conversions/${encodeURIComponent(record.id)}`, {
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    if (response.status === 410) {
      record.status = "expired";
      return;
    }
    if (!response.ok) {
      return;
    }
    const payload = await safeJson(response);
    record.status = normalizeStatus(payload.status) || record.status;
    record.completedAt = stringValue(payload.completedAt) || record.completedAt;
    record.expiresAt = stringValue(payload.expiresAt) || record.expiresAt;
    record.outputName = stringValue(payload.outputName || payload.fileName) || record.outputName;
    record.outputSize = numberValue(payload.outputSize ?? payload.fileSize) || record.outputSize;
    record.errorCode = stringValue(payload.errorCode || payload.code) || record.errorCode;
  }));

  saveHistory();
  renderHistory();
}

function loadHistory() {
  try {
    const raw = localStorage.getItem(HISTORY_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .map(sanitizeRecord)
      .filter((record) => record.id)
      .slice(0, MAX_HISTORY_ITEMS);
  } catch {
    return [];
  }
}

function saveHistory() {
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(state.history.slice(0, MAX_HISTORY_ITEMS)));
  } catch {
    // 浏览器可能禁用了本地存储；转换功能不应因此中断。
  }
}

function sanitizeRecord(record) {
  let status = normalizeStatus(record.status) || "failed";
  const expiresAt = stringValue(record.expiresAt);
  if (status === "succeeded" && expiresAt) {
    const expiresAtMillis = Date.parse(expiresAt);
    if (Number.isFinite(expiresAtMillis) && expiresAtMillis <= Date.now()) {
      status = "expired";
    }
  }
  return {
    id: stringValue(record.id),
    originalName: stringValue(record.originalName),
    inputSize: numberValue(record.inputSize),
    status,
    createdAt: stringValue(record.createdAt),
    completedAt: stringValue(record.completedAt),
    expiresAt,
    outputName: stringValue(record.outputName),
    outputSize: numberValue(record.outputSize),
    errorCode: stringValue(record.errorCode),
  };
}

function findHistory(id) {
  return state.history.find((record) => record.id === id);
}

function upsertHistory(record) {
  const sanitized = sanitizeRecord(record);
  state.history = [
    sanitized,
    ...state.history.filter((item) => item.id !== sanitized.id),
  ].slice(0, MAX_HISTORY_ITEMS);
  saveHistory();
  renderHistory();
}

function clearHistory() {
  state.history = [];
  saveHistory();
  renderHistory();
}

function renderHistory() {
  elements.historyList.replaceChildren();
  elements.historyEmpty.hidden = state.history.length > 0;
  elements.clearHistory.hidden = state.history.length === 0;

  const fragment = document.createDocumentFragment();
  state.history.forEach((record) => {
    const item = elements.historyTemplate.content.firstElementChild.cloneNode(true);
    const name = item.querySelector(".history-name");
    const detail = item.querySelector(".history-detail");
    const status = item.querySelector(".history-status");
    const download = item.querySelector(".history-download");

    name.textContent = record.outputName || record.originalName || "未命名文稿";
    detail.textContent = historyDetail(record);
    status.textContent = statusLabels[record.status] || "状态未知";
    status.dataset.status = record.status;

    if (record.status === "succeeded") {
      download.href = `/api/conversions/${encodeURIComponent(record.id)}/download`;
      if (record.outputName) {
        download.setAttribute("download", record.outputName);
      }
    } else {
      download.hidden = true;
    }
    fragment.appendChild(item);
  });
  elements.historyList.appendChild(fragment);
}

function historyDetail(record) {
  const parts = [];
  if (record.createdAt) {
    parts.push(formatDate(record.createdAt));
  }
  const size = record.outputSize || record.inputSize;
  if (size > 0) {
    parts.push(formatBytes(size));
  }
  if ((record.status === "failed" || record.status === "evicted") && record.errorCode) {
    parts.push(`错误码 ${record.errorCode}`);
  }
  return parts.join(" · ") || "本地转换记录";
}

function normalizeStatus(value) {
  const status = stringValue(value).toLowerCase();
  return [
    "uploading",
    "queued",
    "converting",
    "succeeded",
    "failed",
    "expired",
    "evicted",
  ].includes(status) ? status : "";
}

function terminalStageFromHistory() {
  const record = findHistory(state.currentJobId);
  if (!record) {
    return "uploading";
  }
  if (record.completedAt || record.outputName) {
    return "succeeded";
  }
  return record.status === "failed" ? "converting" : "succeeded";
}

function progressHeading(status) {
  if (status === "succeeded") {
    return "装订完成";
  }
  if (status === "failed") {
    return "装订未完成";
  }
  if (status === "expired" || status === "evicted") {
    return "成品已离架";
  }
  return "正在装订";
}

function terminalMessage(status, errorCode) {
  if (status === "expired") {
    return "这份成品已超过保留期限，请重新上传文稿转换。";
  }
  if (status === "evicted") {
    return "因存储空间清理，这份较早的成品已被删除。";
  }
  return errorCode
    ? `转换没有完成，错误码：${errorCode}。请检查文稿后重试。`
    : "转换没有完成，请检查文稿后重试。";
}

function readableError(payload, statusCode) {
  const errorPayload = payload && typeof payload.error === "object" ? payload.error : {};
  const serverMessage = stringValue(payload.message || errorPayload.message || payload.error);
  if (serverMessage) {
    return serverMessage;
  }
  if (statusCode === 413) {
    return "文稿超过允许的大小，请选择更小的 TXT 文件。";
  }
  if (statusCode === 429) {
    return "你的网络已有任务正在处理，请等待完成后再试。";
  }
  if (statusCode === 503) {
    return "当前等待人数已满，请稍后再试。";
  }
  if (statusCode === 507) {
    return "服务端存储空间不足，请稍后再试。";
  }
  return "请求没有成功，请稍后重试。";
}

function expiryText(expiresAt, outputSize) {
  const parts = [];
  if (outputSize > 0) {
    parts.push(formatBytes(outputSize));
  }
  if (expiresAt) {
    parts.push(`${formatDate(expiresAt)} 失效`);
  } else {
    parts.push("成品将在 24 小时后失效");
  }
  return parts.join(" · ");
}

function formatBytes(value) {
  const bytes = numberValue(value);
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ["KB", "MB", "GB"];
  let size = bytes;
  let unitIndex = -1;
  do {
    size /= 1024;
    unitIndex += 1;
  } while (size >= 1024 && unitIndex < units.length - 1);
  return `${size >= 10 ? size.toFixed(1) : size.toFixed(2)} ${units[unitIndex]}`;
}

function formatDate(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "时间未知";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function shortId(id) {
  return id.length > 12 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

function numberValue(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}

function stringValue(value) {
  return typeof value === "string" ? value : "";
}

async function safeJson(response) {
  try {
    return await response.json();
  } catch {
    return {};
  }
}
