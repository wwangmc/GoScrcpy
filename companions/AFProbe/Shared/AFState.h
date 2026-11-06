#import <Foundation/Foundation.h>
NSString *AFDirectory(NSString *home);
NSDictionary *AFRead(NSString *path);
BOOL AFWrite(NSString *path, NSDictionary *value);
NSDictionary *AFLease(NSString *home);
id AFFind(id value, NSString *field, NSUInteger depth);
NSString *AFDevKey(NSData *data);
void AFCapture(NSString *home, NSString *path);
void AFScan(NSString *home);
