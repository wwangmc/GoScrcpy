export namespace device {
	
	export class Device {
	    serial: string;
	    state: string;
	    model: string;
	    product: string;
	    transport: string;
	    platform: string;
	    error?: string;
	    codec: string;
	    videoModes?: string[];
	    controls?: string[];
	    settings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.state = source["state"];
	        this.model = source["model"];
	        this.product = source["product"];
	        this.transport = source["transport"];
	        this.platform = source["platform"];
	        this.error = source["error"];
	        this.codec = source["codec"];
	        this.videoModes = source["videoModes"];
	        this.controls = source["controls"];
	        this.settings = source["settings"];
	    }
	}

}

export namespace iossettings {
	
	export class Choice {
	    region: string;
	    name: string;
	    locale: string;
	    language: string;
	    timeZone: string;
	    carrierCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Choice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.region = source["region"];
	        this.name = source["name"];
	        this.locale = source["locale"];
	        this.language = source["language"];
	        this.timeZone = source["timeZone"];
	        this.carrierCount = source["carrierCount"];
	    }
	}

}

export namespace main {
	
	export class FileListing {
	    path: string;
	    entries: phone.Entry[];
	    elapsedMS: number;
	    platform: string;
	    scope: string;
	    hint?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileListing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.entries = this.convertValues(source["entries"], phone.Entry);
	        this.elapsedMS = source["elapsedMS"];
	        this.platform = source["platform"];
	        this.scope = source["scope"];
	        this.hint = source["hint"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FileLocation {
	    scope: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new FileLocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.path = source["path"];
	    }
	}
	export class FileTransfer {
	    id: string;
	    name: string;
	    direction: string;
	    done: number;
	    total: number;
	    state: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileTransfer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.direction = source["direction"];
	        this.done = source["done"];
	        this.total = source["total"];
	        this.state = source["state"];
	        this.error = source["error"];
	    }
	}
	export class IOSSettingsInfo {
	    choices: iossettings.Choice[];
	    region: string;
	
	    static createFrom(source: any = {}) {
	        return new IOSSettingsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.choices = this.convertValues(source["choices"], iossettings.Choice);
	        this.region = source["region"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LaunchOptions {
	    lastDevice: string;
	    fileSerial: string;
	    smokeSerial: string;
	    mirrorSerial: string;
	    settings: scrcpy.Options;
	
	    static createFrom(source: any = {}) {
	        return new LaunchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lastDevice = source["lastDevice"];
	        this.fileSerial = source["fileSerial"];
	        this.smokeSerial = source["smokeSerial"];
	        this.mirrorSerial = source["mirrorSerial"];
	        this.settings = this.convertValues(source["settings"], scrcpy.Options);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Mirror {
	    url: string;
	    metadata: scrcpy.Metadata;
	    codec: string;
	    videoModes: string[];
	    controls: string[];
	    settings: string[];
	    pointerIntervalMS: number;
	
	    static createFrom(source: any = {}) {
	        return new Mirror(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.metadata = this.convertValues(source["metadata"], scrcpy.Metadata);
	        this.codec = source["codec"];
	        this.videoModes = source["videoModes"];
	        this.controls = source["controls"];
	        this.settings = source["settings"];
	        this.pointerIntervalMS = source["pointerIntervalMS"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxyChange {
	    message: string;
	    pending: boolean;
	    proxyName: string;
	    region: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.pending = source["pending"];
	        this.proxyName = source["proxyName"];
	        this.region = source["region"];
	    }
	}
	export class ProxyLines {
	    lines: proxyip.PublicLine[];
	    skipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyLines(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lines = this.convertValues(source["lines"], proxyip.PublicLine);
	        this.skipped = source["skipped"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ShellLaunch {
	    missingOpenSSH: boolean;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new ShellLaunch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.missingOpenSSH = source["missingOpenSSH"];
	        this.warning = source["warning"];
	    }
	}
	export class SmokeReport {
	    codec: string;
	    frames: number;
	    width: number;
	    height: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SmokeReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.codec = source["codec"];
	        this.frames = source["frames"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.error = source["error"];
	    }
	}
	export class State {
	    adbPath: string;
	    error: string;
	    serial: string;
	    active: boolean;
	    logs: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adbPath = source["adbPath"];
	        this.error = source["error"];
	        this.serial = source["serial"];
	        this.active = source["active"];
	        this.logs = source["logs"];
	        this.version = source["version"];
	    }
	}

}

export namespace phone {
	
	export class SharedApp {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new SharedApp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}

}

export namespace proxyip {
	
	export class IPResult {
	    missingCurl?: boolean;
	    query: string;
	    country: string;
	    regionName: string;
	    city: string;
	    isp: string;
	    org: string;
	    timezone: string;
	
	    static createFrom(source: any = {}) {
	        return new IPResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.missingCurl = source["missingCurl"];
	        this.query = source["query"];
	        this.country = source["country"];
	        this.regionName = source["regionName"];
	        this.city = source["city"];
	        this.isp = source["isp"];
	        this.org = source["org"];
	        this.timezone = source["timezone"];
	    }
	}
	export class Options {
	    proxyId: string;
	    region: string;
	    appId: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyId = source["proxyId"];
	        this.region = source["region"];
	        this.appId = source["appId"];
	        this.name = source["name"];
	    }
	}
	export class PublicLine {
	    id: string;
	    name: string;
	    hostname: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new PublicLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.hostname = source["hostname"];
	        this.port = source["port"];
	    }
	}

}

export namespace scrcpy {
	
	export class Metadata {
	    name: string;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new Metadata(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class Options {
	    maxSize: number;
	    maxFPS: number;
	    bitRate: number;
	    videoMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxSize = source["maxSize"];
	        this.maxFPS = source["maxFPS"];
	        this.bitRate = source["bitRate"];
	        this.videoMode = source["videoMode"];
	    }
	}

}

