//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <PDFKit/PDFKit.h>
#include <stdint.h>
#include <stdio.h>

int proviewPrintPDF(const char *path, const char *title, uintptr_t parent, char *error, int capacity) {
    __block int result = -1;
    void (^operation)(void) = ^{
        @autoreleasepool {
            PDFDocument *document = nil;
            NSPrintInfo *info = nil;
            @try {
                NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
                document = [[PDFDocument alloc] initWithURL:url];
                if (!document) {
                    snprintf(error, capacity, "The prepared document could not be opened.");
                    return;
                }
                info = [[NSPrintInfo sharedPrintInfo] copy];
                NSPrintOperation *print = [document printOperationForPrintInfo:info
                    scalingMode:kPDFPrintPageScaleToFit autoRotate:YES];
                if (!print) {
                    snprintf(error, capacity, "The print operation could not be created.");
                } else {
                    print.jobTitle = [NSString stringWithUTF8String:title];
                    print.showsPrintPanel = YES;
                    print.showsProgressPanel = YES;
                    print.canSpawnSeparateThread = YES;
                    NSWindow *window = (NSWindow *)(void *)parent;
                    if (window) [window makeKeyAndOrderFront:nil];
                    [NSApp activateIgnoringOtherApps:YES];
                    // runOperation retains the document and its private source
                    // through the dialog and spooling; Go cleans up afterwards.
                    result = [print runOperation] ? 1 : 0;
                }
            } @catch (NSException *exception) {
                snprintf(error, capacity, "%s", [[exception reason] UTF8String]);
                result = -1;
            } @finally {
                [info release];
                [document release];
            }
        }
    };
    if ([NSThread isMainThread]) operation();
    else dispatch_sync(dispatch_get_main_queue(), operation);
    return result;
}
