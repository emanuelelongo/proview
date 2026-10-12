//go:build darwin && !no_native_menus

#import <AppKit/AppKit.h>

// Fyne assigns tags starting at 5000 and exports this state lookup.
extern BOOL menuEnabled(int);

@interface ProviewModelMenuDelegate : NSObject <NSMenuDelegate>
@end
@implementation ProviewModelMenuDelegate
- (void)menuNeedsUpdate:(NSMenu *)menu {
    if (menu.numberOfItems != 12 || ![menu.title isEqualToString:@"View"] ||
        ![[menu itemAtIndex:4].title isEqualToString:@"Isometric"]) return;
    NSMenuItem *firstView = [menu itemAtIndex:4];
    BOOL hidden = !menuEnabled((int)firstView.tag - 5000);
    for (NSInteger i = 3; i < menu.numberOfItems; i++) {
        [[menu itemAtIndex:i] setHidden:hidden];
    }
}
@end

void proviewObserveModelMenus(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        static ProviewModelMenuDelegate *delegate;
        delegate = [ProviewModelMenuDelegate new];
        [[NSNotificationCenter defaultCenter]
            addObserverForName:NSMenuDidAddItemNotification object:nil queue:nil
            usingBlock:^(NSNotification *notification) {
                NSMenu *menu = notification.object;
                if ([menu.title isEqualToString:@"View"]) menu.delegate = delegate;
            }];
    });
}
