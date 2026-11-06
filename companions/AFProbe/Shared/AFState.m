#import "AFState.h"
#include <sys/stat.h>
#include <unistd.h>

NSString *AFDirectory(NSString *home) { return [home stringByAppendingPathComponent:@"Library/Caches/.goscrcpy-afprobe"]; }
NSDictionary *AFRead(NSString *path) {
    NSData *data = [NSData dataWithContentsOfFile:path];
    if (!data || data.length > 65536) return nil;
    id value = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    return [value isKindOfClass:NSDictionary.class] ? value : nil;
}
BOOL AFWrite(NSString *path, NSDictionary *value) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    if (!data || ![data writeToFile:path options:NSDataWritingAtomic error:nil]) return NO;
    chmod(path.fileSystemRepresentation, 0600);
    if (geteuid() == 0) chown(path.fileSystemRepresentation, 501, 501);
    return YES;
}
NSDictionary *AFLease(NSString *home) {
    NSDictionary *lease = AFRead([AFDirectory(home) stringByAppendingPathComponent:@"lease.json"]);
    double now = NSProcessInfo.processInfo.systemUptime;
    if (![lease[@"owner"] isKindOfClass:NSString.class] || [lease[@"owner"] length] != 48 || ![lease[@"expires"] respondsToSelector:@selector(doubleValue)] || [lease[@"expires"] doubleValue] < now || [lease[@"expires"] doubleValue] > now + 35) return nil;
    return lease;
}
id AFFind(id value, NSString *field, NSUInteger depth) {
    if (!value || depth > 10) return nil;
    if ([value isKindOfClass:NSData.class]) {
        if ([value length] > 2*1024*1024) return nil;
        id decoded = [NSJSONSerialization JSONObjectWithData:value options:NSJSONReadingFragmentsAllowed error:nil];
        if (!decoded) decoded = [NSPropertyListSerialization propertyListWithData:value options:NSPropertyListImmutable format:nil error:nil];
        return AFFind(decoded, field, depth+1);
    }
    if ([value isKindOfClass:NSString.class]) {
        NSData *data = [value dataUsingEncoding:NSUTF8StringEncoding];
        if ([value hasPrefix:@"{"] || [value hasPrefix:@"["] || [value hasPrefix:@"<?xml"]) return AFFind(data, field, depth+1);
        return nil;
    }
    if ([value isKindOfClass:NSDictionary.class]) {
        id found = value[field];
        if ([found isKindOfClass:NSString.class] && [found length] > 0 && [found length] <= 256) return found;
        for (id key in value) { found = AFFind(value[key], field, depth+1); if (found) return found; }
    } else if ([value isKindOfClass:NSArray.class]) {
        for (id child in value) { id found = AFFind(child, field, depth+1); if (found) return found; }
    }
    return nil;
}
NSString *AFDevKey(NSData *data) {
    NSString *key = AFFind(data, @"dev_key", 0);
    // AF cache versions may wrap JSON in a binary record. Match the exact JSON
    // field, never similarly named keys; do not unarchive arbitrary objects.
    if (!key && data.length <= 2*1024*1024) {
        NSString *text = [[NSString alloc] initWithData:data encoding:NSISOLatin1StringEncoding];
        NSRegularExpression *regex = [NSRegularExpression regularExpressionWithPattern:@"\"dev_key\"\\s*:\\s*\"([A-Za-z0-9_-]{1,256})\"" options:0 error:nil];
        NSTextCheckingResult *match = [regex firstMatchInString:text ?: @"" options:0 range:NSMakeRange(0, text.length)];
        if (match) key = [text substringWithRange:[match rangeAtIndex:1]];
    }
    if (![key isKindOfClass:NSString.class] || key.length == 0 || key.length > 256 || [key rangeOfCharacterFromSet:[[NSCharacterSet characterSetWithCharactersInString:@"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"] invertedSet]].location != NSNotFound) return nil;
    return key;
}
void AFCapture(NSString *home, NSString *path) {
    NSDictionary *lease = AFLease(home);
    if (!lease) return;
    NSString *base = [[home stringByAppendingPathComponent:@"Library/Caches/appsflyer-v1"] stringByResolvingSymlinksInPath];
    path = [path stringByResolvingSymlinksInPath];
    if (![path hasPrefix:[base stringByAppendingString:@"/"]]) return;
    NSString *resultPath = [AFDirectory(home) stringByAppendingPathComponent:@"found.json"];
    if ([AFRead(resultPath)[@"owner"] isEqual:lease[@"owner"]]) return;
    struct stat info;
    if (lstat(path.fileSystemRepresentation, &info) != 0 || !S_ISREG(info.st_mode) || info.st_size > 2*1024*1024) return;
    NSData *data = [NSData dataWithContentsOfFile:path options:NSDataReadingUncached error:nil];
    NSString *key = AFDevKey(data);
    if (!key) return;
    // Keep only the requested field, not cached payloads or application data.
    NSMutableDictionary *result = [@{@"owner":lease[@"owner"], @"devKey":key, @"file":path.lastPathComponent, @"capturedAt":@([[NSDate date] timeIntervalSince1970])} mutableCopy];
    NSString *appID = AFFind(data, @"app_id", 0);
    if (appID) result[@"appID"] = appID;
    AFWrite(resultPath, result);
}
void AFScan(NSString *home) {
    if (!AFLease(home)) return;
    NSString *base = [home stringByAppendingPathComponent:@"Library/Caches/appsflyer-v1"];
    NSDirectoryEnumerator *files = [NSFileManager.defaultManager enumeratorAtPath:base];
    NSUInteger count = 0;
    for (NSString *relative in files) {
        if (++count > 128) break;
        AFCapture(home, [base stringByAppendingPathComponent:relative]);
    }
}
