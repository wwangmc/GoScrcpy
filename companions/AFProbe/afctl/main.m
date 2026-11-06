#import <Foundation/Foundation.h>
#import "AFState.h"
#include <sys/stat.h>
#include <unistd.h>

static int emit(NSDictionary *value) {
    NSMutableDictionary *out = [value mutableCopy]; out[@"protocol"] = @1;
    NSData *data = [NSJSONSerialization dataWithJSONObject:out options:0 error:nil];
    printf("GSAF1|%s\n", [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
    return [out[@"status"] isEqual:@"error"] ? 1 : 0;
}
static BOOL matches(NSString *s,NSString *pattern) { return [s rangeOfString:pattern options:NSRegularExpressionSearch].location != NSNotFound; }
int main(int argc, char **argv) { @autoreleasepool {
    if (argc == 2 && !strcmp(argv[1],"selftest")) {
        NSArray *values = @[@"{\"dev_key\":\"unit_TEST123\"}", @"{\"data\":\"{\\\"dev_key\\\":\\\"unit_TEST123\\\"}\"}"];
        for (NSString *text in values) if (![AFDevKey([text dataUsingEncoding:NSUTF8StringEncoding]) isEqual:@"unit_TEST123"]) return emit(@{@"status":@"error",@"message":@"parser failed"});
        if (AFDevKey([@"{\"fake_dev_key\":\"x\"}" dataUsingEncoding:NSUTF8StringEncoding])) return emit(@{@"status":@"error"});
        return emit(@{@"status":@"success",@"message":@"parser passed"});
    }
    if (argc != 3) return emit(@{@"status":@"error",@"message":@"invalid arguments"});
    NSString *action = @(argv[1]);
    NSData *data = [[NSData alloc] initWithBase64EncodedString:@(argv[2]) options:0];
    NSDictionary *args = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
    if (![args isKindOfClass:NSDictionary.class]) return emit(@{@"status":@"error",@"message":@"invalid request"});
    NSString *home = args[@"container"], *bundle = args[@"bundle"], *owner = args[@"owner"];
    if (![home isKindOfClass:NSString.class] || !matches(home,@"^/(private/)?var/mobile/Containers/Data/Application/[A-Fa-f0-9-]{36}$") || ![bundle isKindOfClass:NSString.class] || !matches(bundle,@"^[A-Za-z0-9_-]+(\\.[A-Za-z0-9_-]+)+$") || ![owner isKindOfClass:NSString.class] || !matches(owner,@"^[a-f0-9]{48}$")) return emit(@{@"status":@"error",@"message":@"invalid scope"});
    NSDictionary *metadata = [NSDictionary dictionaryWithContentsOfFile:[home stringByAppendingPathComponent:@".com.apple.mobile_container_manager.metadata.plist"]];
    if (![metadata[@"MCMMetadataIdentifier"] isEqual:bundle]) return emit(@{@"status":@"error",@"message":@"应用沙盒已变更，请刷新应用列表"});
    NSString *dir = AFDirectory(home), *leasePath = [dir stringByAppendingPathComponent:@"lease.json"];
    NSDictionary *lease = AFLease(home);
    if (([action isEqual:@"start"] || [action isEqual:@"poll"]) && lease && ![lease[@"owner"] isEqual:owner]) return emit(@{@"status":@"error",@"message":@"此应用正在另一窗口探测"});
    if ([action isEqual:@"stop"]) {
        NSDictionary *stored = AFRead(leasePath);
        if ([stored[@"owner"] isEqual:owner]) {
            for (NSString *name in @[@"lease.json",@"hook.json",@"found.json"]) unlink([[dir stringByAppendingPathComponent:name] fileSystemRepresentation]);
            rmdir(dir.fileSystemRepresentation);
        }
        return emit(@{@"status":@"success"});
    }
    if (![action isEqual:@"info"] && ![action isEqual:@"start"] && ![action isEqual:@"poll"]) return emit(@{@"status":@"error",@"message":@"invalid operation"});
    if (![action isEqual:@"info"]) {
        BOOL directory = NO;
        if ([NSFileManager.defaultManager fileExistsAtPath:dir isDirectory:&directory]) {
            struct stat st;
            if (!directory || lstat(dir.fileSystemRepresentation,&st) != 0 || S_ISLNK(st.st_mode)) return emit(@{@"status":@"error",@"message":@"探测目录不安全"});
        } else if (![NSFileManager.defaultManager createDirectoryAtPath:dir withIntermediateDirectories:YES attributes:nil error:nil]) return emit(@{@"status":@"error",@"message":@"无法创建探测状态目录"});
        chmod(dir.fileSystemRepresentation,0700); chown(dir.fileSystemRepresentation,501,501);
        if (!AFWrite(leasePath,@{@"owner":owner,@"expires":@(NSProcessInfo.processInfo.systemUptime+30)})) return emit(@{@"status":@"error",@"message":@"无法启动探测"});
        AFScan(home);
    }
    NSMutableDictionary *result = [@{@"status":@"success",@"bundle":bundle} mutableCopy];
    NSDictionary *prefs = [NSDictionary dictionaryWithContentsOfFile:[home stringByAppendingPathComponent:[NSString stringWithFormat:@"Library/Preferences/%@.plist",bundle]]];
    id uid = prefs[@"AppsFlyerUserId"];
    if ([uid isKindOfClass:NSString.class] && [uid length] <= 256) result[@"deviceID"] = uid;
    NSDictionary *found = AFRead([dir stringByAppendingPathComponent:@"found.json"]);
    if ([found[@"owner"] isEqual:owner]) {
        for (NSString *key in @[@"devKey",@"file",@"appID",@"capturedAt"]) if (found[key]) result[key] = found[key];
    }
    NSDictionary *hook = AFRead([dir stringByAppendingPathComponent:@"hook.json"]);
    result[@"hooked"] = @((BOOL)([hook[@"owner"] isEqual:owner] && NSProcessInfo.processInfo.systemUptime-[hook[@"uptime"] doubleValue] < 3));
    return emit(result);
}}
