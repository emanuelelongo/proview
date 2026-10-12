#import <AppKit/AppKit.h>
#import <CoreServices/CoreServices.h>
#include <stdlib.h>
#include <string.h>

extern void goProviewOpenFiles(char **paths, int count);

@interface ProviewOpenDocumentsHandler : NSObject
- (void)installHandler;
- (void)willFinishLaunching:(NSNotification *)notification;
- (void)openDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent;
@end

@implementation ProviewOpenDocumentsHandler
- (void)installHandler {
    [[NSAppleEventManager sharedAppleEventManager]
        setEventHandler:self
        andSelector:@selector(openDocuments:withReplyEvent:)
        forEventClass:kCoreEventClass
        andEventID:kAEOpenDocuments];
}

- (void)willFinishLaunching:(NSNotification *)notification {
    // AppKit installs its default handlers during initialization. Reinstall
    // before launch documents arrive without replacing GLFW's app delegate.
    [self installHandler];
}

- (void)openDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
    NSAppleEventDescriptor *documents = [event paramDescriptorForKeyword:keyDirectObject];
    if (documents == nil) {
        return;
    }

    NSInteger count = documents.descriptorType == typeAEList ? documents.numberOfItems : 1;
    if (count <= 0) {
        return;
    }
    char **paths = calloc((size_t)count, sizeof(char *));
    if (paths == NULL) {
        return;
    }

    int pathCount = 0;
    for (NSInteger index = 1; index <= count; index++) {
        NSAppleEventDescriptor *descriptor = documents.descriptorType == typeAEList
            ? [documents descriptorAtIndex:index]
            : documents;
        NSURL *url = descriptor.fileURLValue;
        if (url == nil || !url.isFileURL) {
            continue;
        }
        const char *path = url.fileSystemRepresentation;
        if (path != NULL) {
            paths[pathCount] = strdup(path);
            if (paths[pathCount] != NULL) {
                pathCount++;
            }
        }
    }

    if (pathCount > 0) {
        goProviewOpenFiles(paths, pathCount);
    }
    for (int index = 0; index < pathCount; index++) {
        free(paths[index]);
    }
    free(paths);
}
@end

void proviewRegisterOpenDocumentsHandler(void) {
    static ProviewOpenDocumentsHandler *handler;
    if (handler == nil) {
        handler = [[ProviewOpenDocumentsHandler alloc] init];
        [[NSNotificationCenter defaultCenter]
            addObserver:handler
            selector:@selector(willFinishLaunching:)
            name:NSApplicationWillFinishLaunchingNotification
            object:nil];
    }
    [handler installHandler];
}
