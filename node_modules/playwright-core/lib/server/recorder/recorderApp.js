"use strict";
var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
var recorderApp_exports = {};
__export(recorderApp_exports, {
  ProgrammaticRecorderApp: () => ProgrammaticRecorderApp,
  RecorderApp: () => RecorderApp
});
module.exports = __toCommonJS(recorderApp_exports);
var import_fs = __toESM(require("fs"));
var import_path = __toESM(require("path"));
var import_debug = require("../utils/debug");
var import_utilsBundle = require("../../utilsBundle");
var import_instrumentation = require("../instrumentation");
var import_launchApp = require("../launchApp");
var import_launchApp2 = require("../launchApp");
var import_progress = require("../progress");
var import_throttledFile = require("./throttledFile");
var import_languages = require("../codegen/languages");
var import_recorderUtils = require("./recorderUtils");
var import_language = require("../codegen/language");
var import_recorder = require("../recorder");
var import_time = require("../../utils/isomorphic/time");
var import_browserContext = require("../browserContext");
class RecorderApp {
  constructor(recorder, params, page, wsEndpointForTest) {
    this._throttledOutputFile = null;
    this._actions = [];
    this._userSources = [];
    this._recorderSources = [];
    this._page = page;
    this._recorder = recorder;
    this.wsEndpointForTest = wsEndpointForTest;
    this._languageGeneratorOptions = {
      browserName: params.browserName,
      launchOptions: { headless: false, ...params.launchOptions, tracesDir: void 0 },
      contextOptions: { ...params.contextOptions },
      deviceName: params.device,
      saveStorage: params.saveStorage
    };
    this._throttledOutputFile = params.outputFile ? new import_throttledFile.ThrottledFile(params.outputFile) : null;
    this._primaryLanguage = process.env.TEST_INSPECTOR_LANGUAGE || params.language || params.sdkLanguage;
  }
  async _init(inspectedContext) {
    await (0, import_launchApp.syncLocalStorageWithSettings)(this._page, "recorder");
    const controller = new import_progress.ProgressController((0, import_instrumentation.serverSideCallMetadata)(), this._page);
    await controller.run(async (progress) => {
      await this._page.addRequestInterceptor(progress, (route) => {
        if (!route.request().url().startsWith("https://playwright/")) {
          route.continue({ isFallback: true }).catch(() => {
          });
          return;
        }
        const uri = route.request().url().substring("https://playwright/".length);
        const file = require.resolve("../../vite/recorder/" + uri);
        import_fs.default.promises.readFile(file).then((buffer) => {
          route.fulfill({
            status: 200,
            headers: [
              { name: "Content-Type", value: import_utilsBundle.mime.getType(import_path.default.extname(file)) || "application/octet-stream" }
            ],
            body: buffer.toString("base64"),
            isBase64: true
          }).catch(() => {
          });
        });
      });
      await this._page.exposeBinding(progress, "dispatch", false, (_, data) => this._handleUIEvent(data));
      this._page.once("close", () => {
        this._recorder.close();
        this._page.browserContext.close({ reason: "Recorder window closed" }).catch(() => {
        });
        delete inspectedContext[recorderAppSymbol];
      });
      await this._page.mainFrame().goto(progress, process.env.PW_HMR ? "http://localhost:44225" : "https://playwright/index.html");
    });
    const url = this._recorder.url();
    if (url)
      this._onPageNavigated(url);
    this._onModeChanged(this._recorder.mode());
    this._onPausedStateChanged(this._recorder.paused());
    this._onUserSourcesChanged(this._recorder.userSources());
    this._onCallLogsUpdated(this._recorder.callLog());
    this._wireListeners(this._recorder);
    this._updateActions(true);
  }
  _handleUIEvent(data) {
    if (data.event === "clear") {
      this._actions = [];
      this._updateActions();
      this._recorder.clear();
      return;
    }
    if (data.event === "fileChanged") {
      const source = [...this._recorderSources, ...this._userSources].find((s) => s.id === data.params.fileId);
      if (source)
        this._recorder.setLanguage(source.language);
      return;
    }
    if (data.event === "setMode") {
      this._recorder.setMode(data.params.mode);
      return;
    }
    if (data.event === "resume") {
      this._recorder.resume();
      return;
    }
    if (data.event === "pause") {
      this._recorder.pause();
      return;
    }
    if (data.event === "step") {
      this._recorder.step();
      return;
    }
    if (data.event === "highlightRequested") {
      if (data.params.selector)
        this._recorder.setHighlightedSelector(data.params.selector);
      if (data.params.ariaTemplate)
        this._recorder.setHighlightedAriaTemplate(data.params.ariaTemplate);
      return;
    }
    throw new Error(`Unknown event: ${data.event}`);
  }
  static async show(context, params) {
    if (process.env.PW_CODEGEN_NO_INSPECTOR)
      return;
    const recorder = await import_recorder.Recorder.forContext(context, params);
    if (params.recorderMode === "api") {
      await ProgrammaticRecorderApp.run(context, recorder);
      return;
    }
    await RecorderApp._show(recorder, context, params);
  }
  async close() {
    await this._page.close();
  }
  static showInspectorNoReply(context) {
    if (process.env.PW_CODEGEN_NO_INSPECTOR)
      return;
    void import_recorder.Recorder.forContext(context, {}).then((recorder) => RecorderApp._show(recorder, context, {})).catch(() => {
    });
  }
  static async _show(recorder, inspectedContext, params) {
    if (inspectedContext[recorderAppSymbol])
      return;
    inspectedContext[recorderAppSymbol] = true;
    const sdkLanguage = inspectedContext._browser.sdkLanguage();
    const headed = !!inspectedContext._browser.options.headful;
    const recorderPlaywright = require("../playwright").createPlaywright({ sdkLanguage: "javascript", isInternalPlaywright: true });
    const { context: appContext, page } = await (0, import_launchApp2.launchApp)(recorderPlaywright.chromium, {
      sdkLanguage,
      windowSize: { width: 600, height: 600 },
      windowPosition: { x: 1020, y: 10 },
      persistentContextOptions: {
        noDefaultViewport: true,
        headless: !!process.env.PWTEST_CLI_HEADLESS || (0, import_debug.isUnderTest)() && !headed,
        cdpPort: (0, import_debug.isUnderTest)() ? 0 : void 0,
        handleSIGINT: params.handleSIGINT,
        executablePath: inspectedContext._browser.options.isChromium ? inspectedContext._browser.options.customExecutablePath : void 0,
        // Use the same channel as the inspected context to guarantee that the browser is installed.
        channel: inspectedContext._browser.options.isChromium ? inspectedContext._browser.options.channel : void 0
      }
    });
    const controller = new import_progress.ProgressController((0, import_instrumentation.serverSideCallMetadata)(), appContext._browser);
    await controller.run(async (progress) => {
      await appContext._browser._defaultContext._loadDefaultContextAsIs(progress);
    });
    const appParams = {
      browserName: inspectedContext._browser.options.name,
      sdkLanguage: inspectedContext._browser.sdkLanguage(),
      wsEndpointForTest: inspectedContext._browser.options.wsEndpoint,
      headed: !!inspectedContext._browser.options.headful,
      executablePath: inspectedContext._browser.options.isChromium ? inspectedContext._browser.options.customExecutablePath : void 0,
      channel: inspectedContext._browser.options.isChromium ? inspectedContext._browser.options.channel : void 0,
      ...params
    };
    const recorderApp = new RecorderApp(recorder, appParams, page, appContext._browser.options.wsEndpoint);
    await recorderApp._init(inspectedContext);
    inspectedContext.recorderAppForTest = recorderApp;
  }
  _wireListeners(recorder) {
    recorder.on(import_recorder.RecorderEvent.ActionAdded, (action) => {
      this._onActionAdded(action);
    });
    recorder.on(import_recorder.RecorderEvent.SignalAdded, (signal) => {
      this._onSignalAdded(signal);
    });
    recorder.on(import_recorder.RecorderEvent.PageNavigated, (url) => {
      this._onPageNavigated(url);
    });
    recorder.on(import_recorder.RecorderEvent.ContextClosed, () => {
      this._onContextClosed();
    });
    recorder.on(import_recorder.RecorderEvent.ModeChanged, (mode) => {
      this._onModeChanged(mode);
    });
    recorder.on(import_recorder.RecorderEvent.PausedStateChanged, (paused) => {
      this._onPausedStateChanged(paused);
    });
    recorder.on(import_recorder.RecorderEvent.UserSourcesChanged, (sources) => {
      this._onUserSourcesChanged(sources);
    });
    recorder.on(import_recorder.RecorderEvent.ElementPicked, (elementInfo, userGesture) => {
      this._onElementPicked(elementInfo, userGesture);
    });
    recorder.on(import_recorder.RecorderEvent.CallLogsUpdated, (callLogs) => {
      this._onCallLogsUpdated(callLogs);
    });
  }
  _onActionAdded(action) {
    this._actions.push(action);
    this._updateActions();
  }
  _onSignalAdded(signal) {
    const lastAction = this._actions.findLast((a) => a.frame.pageGuid === signal.frame.pageGuid);
    if (lastAction)
      lastAction.action.signals.push(signal.signal);
    this._updateActions();
  }
  _onPageNavigated(url) {
    this._page.mainFrame().evaluateExpression((({ url: url2 }) => {
      window.playwrightSetPageURL(url2);
    }).toString(), { isFunction: true }, { url }).catch(() => {
    });
  }
  _onContextClosed() {
    this._throttledOutputFile?.flush();
    this._page.browserContext.close({ reason: "Recorder window closed" }).catch(() => {
    });
  }
  _onModeChanged(mode) {
    this._page.mainFrame().evaluateExpression(((mode2) => {
      window.playwrightSetMode(mode2);
    }).toString(), { isFunction: true }, mode).catch(() => {
    });
  }
  _onPausedStateChanged(paused) {
    this._page.mainFrame().evaluateExpression(((paused2) => {
      window.playwrightSetPaused(paused2);
    }).toString(), { isFunction: true }, paused).catch(() => {
    });
  }
  _onUserSourcesChanged(sources) {
    if (!sources.length && !this._userSources.length)
      return;
    this._userSources = sources;
    this._pushAllSources();
  }
  _onElementPicked(elementInfo, userGesture) {
    if (userGesture)
      this._page.bringToFront();
    this._page.mainFrame().evaluateExpression(((param) => {
      window.playwrightElementPicked(param.elementInfo, param.userGesture);
    }).toString(), { isFunction: true }, { elementInfo, userGesture }).catch(() => {
    });
  }
  _onCallLogsUpdated(callLogs) {
    this._page.mainFrame().evaluateExpression(((callLogs2) => {
      window.playwrightUpdateLogs(callLogs2);
    }).toString(), { isFunction: true }, callLogs).catch(() => {
    });
  }
  async _pushAllSources() {
    const sources = [...this._userSources, ...this._recorderSources];
    this._page.mainFrame().evaluateExpression((({ sources: sources2 }) => {
      window.playwrightSetSources(sources2);
    }).toString(), { isFunction: true }, { sources }).catch(() => {
    });
    if (process.env.PWTEST_CLI_IS_UNDER_TEST && sources.length) {
      const primarySource = sources.find((s) => s.isPrimary);
      if (process._didSetSourcesForTest(primarySource?.text ?? ""))
        this._page.close().catch(() => {
        });
    }
  }
  _updateActions(initial = false) {
    const timestamp = initial ? 0 : (0, import_time.monotonicTime)();
    const recorderSources = [];
    const actions = (0, import_recorderUtils.collapseActions)(this._actions);
    for (const languageGenerator of (0, import_languages.languageSet)()) {
      const { header, footer, actionTexts, text } = (0, import_language.generateCode)(actions, languageGenerator, this._languageGeneratorOptions);
      const source = {
        isPrimary: languageGenerator.id === this._primaryLanguage,
        timestamp,
        isRecorded: true,
        label: languageGenerator.name,
        group: languageGenerator.groupName,
        id: languageGenerator.id,
        text,
        header,
        footer,
        actions: actionTexts,
        language: languageGenerator.highlighter,
        highlight: []
      };
      source.revealLine = text.split("\n").length - 1;
      recorderSources.push(source);
      if (languageGenerator.id === this._primaryLanguage)
        this._throttledOutputFile?.setContent(source.text);
    }
    this._recorderSources = recorderSources;
    this._pushAllSources();
  }
}
class ProgrammaticRecorderApp {
  static async run(inspectedContext, recorder) {
    let lastAction = null;
    recorder.on(import_recorder.RecorderEvent.ActionAdded, (action) => {
      const page = findPageByGuid(inspectedContext, action.frame.pageGuid);
      if (!page)
        return;
      if (!lastAction || !(0, import_recorderUtils.shouldMergeAction)(action, lastAction))
        inspectedContext.emit(import_browserContext.BrowserContext.Events.RecorderEvent, { event: "actionAdded", data: action, page });
      else
        inspectedContext.emit(import_browserContext.BrowserContext.Events.RecorderEvent, { event: "actionUpdated", data: action, page });
      lastAction = action;
    });
    recorder.on(import_recorder.RecorderEvent.SignalAdded, (signal) => {
      const page = findPageByGuid(inspectedContext, signal.frame.pageGuid);
      inspectedContext.emit(import_browserContext.BrowserContext.Events.RecorderEvent, { event: "signalAdded", data: signal, page });
    });
  }
}
function findPageByGuid(context, guid) {
  return context.pages().find((p) => p.guid === guid);
}
const recorderAppSymbol = Symbol("recorderApp");
// Annotate the CommonJS export names for ESM import in node:
0 && (module.exports = {
  ProgrammaticRecorderApp,
  RecorderApp
});
