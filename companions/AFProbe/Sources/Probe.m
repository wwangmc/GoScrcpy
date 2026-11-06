#import <Foundation/Foundation.h>
#import <objc/runtime.h>
#import "AFState.h"
#include "fishhook.h"
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>

static NSString *home, *cachePrefix;
static __thread BOOL inside;
static dispatch_source_t timer;
static int (*oldUnlink)(const char *), (*oldRemove)(const char *);
static int (*oldUnlinkAt)(int, const char *, int);
static int (*oldRename)(const char *, const char *);
static BOOL (*oldRemovePath)(id, SEL, NSString *, NSError **);
static BOOL (*oldRemoveURL)(id, SEL, NSURL *, NSError **);

static void capturePath(NSString *path) {
    if (inside || !path) return;
    // Cheap dormant check before reading lease files. All other application IO
    // proceeds through the original implementation without data inspection.
    if (![path containsString:@"/Library/Caches/appsflyer-v1"]) return;
    int saved = errno;
    inside = YES;
    @try {
        NSString *resolved = [path stringByResolvingSymlinksInPath];
        if ([resolved isEqual:cachePrefix]) AFScan(home);
        else if ([resolved hasPrefix:[cachePrefix stringByAppendingString:@"/"]]) {
            AFCapture(home, resolved);
            // removeItemAtPath may be deleting a subdirectory.
            BOOL directory = NO;
            if ([NSFileManager.defaultManager fileExistsAtPath:resolved isDirectory:&directory] && directory) AFScan(home);
        }
    } @catch (__unused NSException *e) { } @finally { inside = NO; errno = saved; }
}
static NSString *atPath(int fd, const char *p) {
    if (!p) return nil;
    NSString *path = [NSString stringWithUTF8String:p];
    if ([path isAbsolutePath]) return path;
    if (fd == AT_FDCWD) return [NSFileManager.defaultManager.currentDirectoryPath stringByAppendingPathComponent:path];
    char directory[PATH_MAX];
    if (fcntl(fd, F_GETPATH, directory) != 0) return nil;
    return [[NSString stringWithUTF8String:directory] stringByAppendingPathComponent:path];
}
static int afUnlink(const char *p) { @autoreleasepool { capturePath(atPath(AT_FDCWD,p)); } return oldUnlink(p); }
static int afRemove(const char *p) { @autoreleasepool { capturePath(atPath(AT_FDCWD,p)); } return oldRemove(p); }
static int afUnlinkAt(int fd,const char *p,int flags) { @autoreleasepool { capturePath(atPath(fd,p)); } return oldUnlinkAt(fd,p,flags); }
static int afRename(const char *a,const char *b) { @autoreleasepool { capturePath(atPath(AT_FDCWD,a)); capturePath(atPath(AT_FDCWD,b)); } return oldRename(a,b); }
static BOOL afRemovePath(id object,SEL sel,NSString *path,NSError **error) { capturePath(path); return oldRemovePath(object,sel,path,error); }
static BOOL afRemoveURL(id object,SEL sel,NSURL *url,NSError **error) { if (url.isFileURL) capturePath(url.path); return oldRemoveURL(object,sel,url,error); }

__attribute__((constructor)) static void initProbe(void) {
    @autoreleasepool {
        home = NSHomeDirectory();
        if (![home containsString:@"/Containers/Data/Application/"] || !NSBundle.mainBundle.bundleIdentifier) return;
        cachePrefix = [[home stringByAppendingPathComponent:@"Library/Caches/appsflyer-v1"] stringByResolvingSymlinksInPath];
        // Keep typed original function pointers instead of copying differently
        // signed GOT pointers back into C function-pointer storage.
        struct rebinding hooks[] = {{"unlink",afUnlink,NULL},{"unlinkat",afUnlinkAt,NULL},{"remove",afRemove,NULL},{"rename",afRename,NULL}};
        oldUnlink = unlink; oldUnlinkAt = unlinkat; oldRemove = remove; oldRename = rename;
#if !defined(__arm64e__)
        tm_rebind_symbols(hooks, sizeof(hooks)/sizeof(hooks[0]));
#else
        // Do not write unsigned replacements into authenticated arm64e slots.
        // Foundation deletion hooks below capture the cache before removal;
        // the scan also covers existing files. Direct syscalls remain outside
        // this supported interception path.
        (void)hooks;
#endif
        Method method = class_getInstanceMethod(NSFileManager.class,@selector(removeItemAtPath:error:));
        if (method) oldRemovePath = (void *)method_setImplementation(method,(IMP)afRemovePath);
        method = class_getInstanceMethod(NSFileManager.class,@selector(removeItemAtURL:error:));
        if (method) oldRemoveURL = (void *)method_setImplementation(method,(IMP)afRemoveURL);
        timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER,0,0,dispatch_get_global_queue(QOS_CLASS_UTILITY,0));
        dispatch_source_set_timer(timer,dispatch_time(DISPATCH_TIME_NOW,0),200*NSEC_PER_MSEC,40*NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{ @autoreleasepool {
            NSDictionary *lease = AFLease(home);
            if (!lease) return;
            inside = YES;
            AFWrite([AFDirectory(home) stringByAppendingPathComponent:@"hook.json"], @{@"owner":lease[@"owner"], @"pid":@(getpid()), @"uptime":@(NSProcessInfo.processInfo.systemUptime)});
            AFScan(home);
            inside = NO;
        }});
        dispatch_resume(timer);
    }
}
