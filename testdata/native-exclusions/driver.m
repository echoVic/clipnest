#import <Cocoa/Cocoa.h>
#include <stdatomic.h>

// Settings controls are driven in-process on the main thread. The system's
// remote file list must be operated by a person or a trusted external UI tool.
static id findElement(id root, NSString *label, BOOL actionable, NSUInteger depth) {
    if (!root || depth > 24) return nil;
    NSString *name = [root respondsToSelector:@selector(accessibilityLabel)]
        ? [root accessibilityLabel] : nil;
    NSString *title = [root respondsToSelector:@selector(accessibilityTitle)]
        ? [root accessibilityTitle] : nil;
    id value = [root respondsToSelector:@selector(accessibilityValue)]
        ? [root accessibilityValue] : nil;
    BOOL matches = [name isEqualToString:label] || [title isEqualToString:label] ||
        ([value isKindOfClass:NSString.class] && [value isEqualToString:label]);
    if (matches && (!actionable ||
        ([root respondsToSelector:@selector(accessibilityPerformPress)] &&
         ![[root accessibilityRole] isEqualToString:NSAccessibilityStaticTextRole]))) {
        return root;
    }
    NSArray *children = [root respondsToSelector:@selector(accessibilityChildren)]
        ? [root accessibilityChildren] : nil;
    for (id child in children) {
        id found = findElement(child, label, actionable, depth + 1);
        if (found) return found;
    }
    return nil;
}

int e2e_has_text(void *window, const char *label) {
    @autoreleasepool {
        NSWindow *target = (__bridge NSWindow *)window;
        // Finish AppKit's pending drawing before reading the accessibility
        // tree; never mutate preferences state or request a new invalidation.
        [target displayIfNeeded];
        return findElement(target,
                           [NSString stringWithUTF8String:label], NO, 0) != nil;
    }
}

int e2e_press(void *window, const char *label) {
    @autoreleasepool {
        id item = findElement((__bridge NSWindow *)window,
                             [NSString stringWithUTF8String:label], YES, 0);
        if (!item) return 0;
        if ([item respondsToSelector:@selector(isAccessibilityEnabled)] &&
            ![item isAccessibilityEnabled]) return 0;
        return [item accessibilityPerformPress] ? 1 : 0;
    }
}

static void describeTree(id root, NSMutableString *text, NSUInteger depth) {
    if (!root || depth > 24) return;
    id title = [root respondsToSelector:@selector(accessibilityTitle)] ? [root accessibilityTitle] : nil;
    id value = [root respondsToSelector:@selector(accessibilityValue)] ? [root accessibilityValue] : nil;
    id label = [root respondsToSelector:@selector(accessibilityLabel)] ? [root accessibilityLabel] : nil;
    [text appendFormat:@"%@%@ title=%@ label=%@ value=%@\n",
        [@"" stringByPaddingToLength:depth withString:@" " startingAtIndex:0],
        NSStringFromClass([root class]), title, label, value];
    NSArray *children = [root respondsToSelector:@selector(accessibilityChildren)] ? [root accessibilityChildren] : nil;
    for (id child in children) describeTree(child, text, depth + 1);
}

void e2e_dump(void *window, const char *path) {
    @autoreleasepool {
        NSMutableString *text = [NSMutableString string];
        // Only the acceptance window, never the app menu's recent-file data.
        describeTree((__bridge NSWindow *)window, text, 0);
        [text writeToFile:[NSString stringWithUTF8String:path] atomically:YES
                encoding:NSUTF8StringEncoding error:NULL];
    }
}

// AppKit calls validateURL when the user confirms, not when the panel is
// cancelled. Return YES preserves default validation. Do not retain the panel:
// the production completion/quit path must remain responsible for its lifetime.
static atomic_int confirmation;
@interface E2EConfirmationObserver : NSObject <NSOpenSavePanelDelegate>
@property(copy) NSString *expectedPath;
@property BOOL quitOnConfirmation;
@end

@implementation E2EConfirmationObserver
- (BOOL)panel:(id)sender validateURL:(NSURL *)url error:(NSError **)outError {
    (void)sender;
    (void)outError;
    BOOL matches = [url.path.stringByStandardizingPath isEqualToString:self.expectedPath];
    atomic_store(&confirmation, matches ? 1 : -1);
    if (self.quitOnConfirmation) {
        dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
    }
    return YES;
}
@end

static E2EConfirmationObserver *observer;
int e2e_observe_confirmation(void *panel, const char *expectedPath, int quit) {
    @autoreleasepool {
        NSOpenPanel *picker = (__bridge NSOpenPanel *)panel;
        if (picker.delegate) return 0;
        observer = [E2EConfirmationObserver new];
        observer.expectedPath = [NSString stringWithUTF8String:expectedPath].stringByStandardizingPath;
        observer.quitOnConfirmation = quit != 0;
        atomic_store(&confirmation, 0);
        picker.delegate = observer;
        return 1;
    }
}

// Atomic: the acceptance worker may check this after the main run loop exits.
int e2e_confirmation(void) { return atomic_load(&confirmation); }

void e2e_cancel(void *panel) {
    [(__bridge NSOpenPanel *)panel cancel:nil];
}
