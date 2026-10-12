#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "cocoa_darwin.h"

// Implemented in Go (mac_darwin.go).
extern void goAction(char *name, char *arg);

static void act(NSString *name, NSString *arg) {
    goAction((char *)[name UTF8String], (char *)[(arg ? arg : @"") UTF8String]);
}

static NSString *S(const char *c) { return c ? [NSString stringWithUTF8String:c] : @""; }

static void onMain(void (^block)(void)) {
    if ([NSThread isMainThread]) {
        block();
    } else {
        dispatch_sync(dispatch_get_main_queue(), block);
    }
}

static void later(void (^block)(void)) {
    dispatch_async(dispatch_get_main_queue(), block);
}

// ---------------------------------------------------------------- tables

@interface HSTable : NSObject <NSTableViewDataSource, NSTableViewDelegate>
@property(strong) NSArray<NSArray<NSString *> *> *rows;
@property(copy) void (^onSelect)(NSInteger row);
@end

@implementation HSTable
- (NSInteger)numberOfRowsInTableView:(NSTableView *)t { return (NSInteger)self.rows.count; }
- (id)tableView:(NSTableView *)t objectValueForTableColumn:(NSTableColumn *)c row:(NSInteger)r {
    NSInteger i = [t.tableColumns indexOfObject:c];
    NSArray *cells = self.rows[r];
    return i >= 0 && (NSUInteger)i < cells.count ? cells[i] : @"";
}
- (void)tableViewSelectionDidChange:(NSNotification *)n {
    NSTableView *t = n.object;
    if (self.onSelect) self.onSelect(t.selectedRow);
}
@end

// ---------------------------------------------------------------- controller

@interface HSCtl : NSObject <NSApplicationDelegate, NSWindowDelegate>
@end

static NSWindow *win;
static NSTextField *statusLabel, *jobLabel;
static NSTextView *jobLog, *healthText, *logText, *settingsText;
static NSTableView *siteTable, *svcTable;
static HSTable *siteData, *svcData;
static NSArray<NSString *> *siteNames;
static NSString *selectedName;
static NSMutableDictionary<NSString *, NSButton *> *buttons;
static NSPopUpButton *phpPop, *logPop;
static NSButton *siteAuto, *loginCheck;
static NSArray<NSString *> *shownPHP, *shownLogs;
static NSStatusItem *statusItem;
static HSCtl *ctl;
static BOOL quitting;

@implementation HSCtl
- (void)clicked:(NSButton *)b {
    if ([b.identifier isEqualToString:@"switchphp"]) {
        act(b.identifier, phpPop.titleOfSelectedItem);
    } else if ([b.identifier isEqualToString:@"logrefresh"]) {
        act(b.identifier, logPop.titleOfSelectedItem);
    } else {
        act(b.identifier, @"");
    }
}
- (void)checked:(NSButton *)b { act(b.identifier, b.state == NSControlStateValueOn ? @"1" : @"0"); }
- (void)picked:(NSPopUpButton *)p { act(p.identifier, p.titleOfSelectedItem); }
- (void)menuItem:(NSMenuItem *)m { act(m.representedObject, @""); }
- (BOOL)windowShouldClose:(NSWindow *)w {
    // Closing the window keeps the servers (and the menu bar icon) running.
    [win orderOut:nil];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    return NO;
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)s {
    if (quitting) return NSTerminateNow;
    act(@"quit", @"");
    return NSTerminateCancel;
}
@end

// ---------------------------------------------------------------- building blocks

static NSButton *makeButton(NSString *title, NSString *action) {
    NSButton *b = [NSButton buttonWithTitle:title target:ctl action:@selector(clicked:)];
    b.identifier = action;
    b.bezelStyle = NSBezelStyleRounded;
    if (action) buttons[action] = b;
    return b;
}

static NSButton *makeCheck(NSString *title, NSString *action) {
    NSButton *b = [NSButton checkboxWithTitle:title target:ctl action:@selector(checked:)];
    b.identifier = action;
    return b;
}

static NSTextField *makeLabel(NSString *s) {
    NSTextField *l = [NSTextField labelWithString:s];
    l.lineBreakMode = NSLineBreakByTruncatingTail;
    return l;
}

static NSScrollView *makeText(NSTextView *__strong *out) {
    NSScrollView *sv = [[NSScrollView alloc] initWithFrame:NSMakeRect(0, 0, 400, 100)];
    sv.hasVerticalScroller = YES;
    sv.borderType = NSBezelBorder;
    NSTextView *tv = [[NSTextView alloc] initWithFrame:NSMakeRect(0, 0, 400, 100)];
    tv.editable = NO;
    tv.selectable = YES;
    tv.font = [NSFont monospacedSystemFontOfSize:12 weight:NSFontWeightRegular];
    tv.autoresizingMask = NSViewWidthSizable;
    tv.verticallyResizable = YES;
    tv.horizontallyResizable = NO;
    tv.textContainer.widthTracksTextView = YES;
    sv.documentView = tv;
    *out = tv;
    return sv;
}

static NSScrollView *makeTable(NSTableView *__strong *out, HSTable *data, NSArray *titles, NSArray *widths) {
    NSScrollView *sv = [[NSScrollView alloc] initWithFrame:NSMakeRect(0, 0, 500, 300)];
    sv.hasVerticalScroller = YES;
    sv.borderType = NSBezelBorder;
    NSTableView *t = [[NSTableView alloc] initWithFrame:sv.bounds];
    for (NSUInteger i = 0; i < titles.count; i++) {
        NSTableColumn *c = [[NSTableColumn alloc] initWithIdentifier:[NSString stringWithFormat:@"c%lu", (unsigned long)i]];
        c.title = titles[i];
        c.width = [widths[i] doubleValue];
        c.minWidth = 40;
        [t addTableColumn:c];
    }
    t.dataSource = data;
    t.delegate = data;
    t.allowsEmptySelection = NO;
    t.usesAlternatingRowBackgroundColors = YES;
    sv.documentView = t;
    *out = t;
    return sv;
}

static NSStackView *vstack(NSArray *views, CGFloat spacing) {
    NSStackView *s = [NSStackView stackViewWithViews:views];
    s.orientation = NSUserInterfaceLayoutOrientationVertical;
    s.alignment = NSLayoutAttributeLeading;
    s.spacing = spacing;
    return s;
}

static NSStackView *hstack(NSArray *views, CGFloat spacing) {
    NSStackView *s = [NSStackView stackViewWithViews:views];
    s.orientation = NSUserInterfaceLayoutOrientationHorizontal;
    s.alignment = NSLayoutAttributeCenterY;
    s.spacing = spacing;
    return s;
}

static void pin(NSView *child, NSView *parent, CGFloat inset) {
    child.translatesAutoresizingMaskIntoConstraints = NO;
    [parent addSubview:child];
    [NSLayoutConstraint activateConstraints:@[
        [child.leadingAnchor constraintEqualToAnchor:parent.leadingAnchor constant:inset],
        [child.trailingAnchor constraintEqualToAnchor:parent.trailingAnchor constant:-inset],
        [child.topAnchor constraintEqualToAnchor:parent.topAnchor constant:inset],
        [child.bottomAnchor constraintEqualToAnchor:parent.bottomAnchor constant:-inset],
    ]];
}

static void grow(NSView *v) {
    v.translatesAutoresizingMaskIntoConstraints = NO;
    [v setContentHuggingPriority:1 forOrientation:NSLayoutConstraintOrientationHorizontal];
    [v setContentHuggingPriority:1 forOrientation:NSLayoutConstraintOrientationVertical];
    [v setContentCompressionResistancePriority:1 forOrientation:NSLayoutConstraintOrientationHorizontal];
    [v setContentCompressionResistancePriority:1 forOrientation:NSLayoutConstraintOrientationVertical];
}

static NSTabViewItem *tab(NSString *title, NSView *content) {
    NSTabViewItem *it = [[NSTabViewItem alloc] initWithIdentifier:title];
    it.label = title;
    NSView *host = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 600, 400)];
    pin(content, host, 10);
    it.view = host;
    return it;
}

// ---------------------------------------------------------------- the window

static void buildWindow(void) {
    buttons = [NSMutableDictionary dictionary];
    siteData = [HSTable new];
    svcData = [HSTable new];
    siteData.rows = @[];
    svcData.rows = @[];
    siteNames = @[];
    siteData.onSelect = ^(NSInteger row) {
        if (row >= 0 && (NSUInteger)row < siteNames.count) {
            selectedName = siteNames[row];
            act(@"select", selectedName);
        }
    };

    win = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 960, 640)
                                      styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
                                        backing:NSBackingStoreBuffered
                                          defer:NO];
    win.title = @"Hyper Stack";
    win.delegate = ctl;
    win.releasedWhenClosed = NO;
    win.minSize = NSMakeSize(820, 560);
    [win center];

    // header
    statusLabel = makeLabel(@"Loading...");
    [statusLabel setContentCompressionResistancePriority:1 forOrientation:NSLayoutConstraintOrientationHorizontal];
    NSView *spacer = [NSView new];
    [spacer setContentHuggingPriority:1 forOrientation:NSLayoutConstraintOrientationHorizontal];
    NSStackView *header = hstack(@[statusLabel, spacer, makeButton(@"Install...", @"install"), makeButton(@"Start all", @"startall"),
                                    makeButton(@"Stop all", @"stopall"), makeButton(@"Open www folder", @"www")], 8);
    header.distribution = NSStackViewDistributionFill;
    buttons[@"install"].hidden = YES;

    // Projects
    NSScrollView *sites = makeTable(&siteTable, siteData, @[@"Project", @"Address", @"PHP", @"Status", @"Auto-start"], @[@150, @210, @50, @120, @80]);
    grow(sites);
    phpPop = [[NSPopUpButton alloc] initWithFrame:NSZeroRect pullsDown:NO];
    phpPop.identifier = @"phpchoice";
    siteAuto = makeCheck(@"Start with Hyper Stack", @"siteauto");
    NSStackView *side = vstack(@[makeButton(@"Start", @"start"), makeButton(@"Stop", @"stop"), makeButton(@"Open site", @"open"),
                                  makeButton(@"phpMyAdmin", @"pma"), makeButton(@"Import SQL...", @"import"), makeButton(@"Open folder", @"folder"),
                                  makeLabel(@"PHP version:"), phpPop, makeButton(@"Switch PHP", @"switchphp"), siteAuto,
                                  makeButton(@"Remove from list", @"forget")], 8);
    for (NSView *v in side.views) {
        if ([v isKindOfClass:[NSButton class]] && v != siteAuto) {
            [v.widthAnchor constraintEqualToConstant:170].active = YES;
        }
    }
    [phpPop.widthAnchor constraintEqualToConstant:170].active = YES;
    NSStackView *projects = hstack(@[sites, side], 12);
    projects.alignment = NSLayoutAttributeTop;

    // Services
    NSScrollView *svcs = makeTable(&svcTable, svcData, @[@"Service", @"Status", @"Port", @"Process"], @[@220, @120, @90, @90]);
    grow(svcs);

    // Health
    NSScrollView *healthScroll = makeText(&healthText);
    grow(healthScroll);
    NSStackView *health = vstack(@[hstack(@[makeButton(@"Check now", @"check"), makeButton(@"Apply safe fixes", @"fix")], 8), healthScroll], 8);

    // Logs
    logPop = [[NSPopUpButton alloc] initWithFrame:NSZeroRect pullsDown:NO];
    logPop.identifier = @"logfile";
    logPop.target = ctl;
    logPop.action = @selector(picked:);
    NSScrollView *logScroll = makeText(&logText);
    grow(logScroll);
    NSStackView *logs = vstack(@[hstack(@[makeLabel(@"File:"), logPop, makeButton(@"Refresh", @"logrefresh")], 8), logScroll], 8);

    // Settings
    loginCheck = makeCheck(@"Start Hyper Stack when I sign in", @"login");
    NSScrollView *setScroll = makeText(&settingsText);
    grow(setScroll);
    NSStackView *settings = vstack(@[loginCheck, setScroll, makeLabel(@"To change ports run: hyper config set http_port 8080 (or https_port, mysql_port), then restart the services.")], 8);

    NSTabView *tabs = [[NSTabView alloc] initWithFrame:NSMakeRect(0, 0, 900, 400)];
    NSArray *pairs = @[@[@"Projects", projects], @[@"Services", svcs], @[@"Health check", health], @[@"Logs", logs], @[@"Settings", settings]];
    for (NSArray *p in pairs) [tabs addTabViewItem:tab(p[0], p[1])];
    grow(tabs);

    jobLabel = makeLabel(@"");
    NSScrollView *jobScroll = makeText(&jobLog);
    jobScroll.translatesAutoresizingMaskIntoConstraints = NO;
    [jobScroll.heightAnchor constraintEqualToConstant:120].active = YES;

    NSStackView *root = vstack(@[header, tabs, jobLabel, jobScroll], 8);
    root.alignment = NSLayoutAttributeWidth;
    root.edgeInsets = NSEdgeInsetsMake(12, 12, 12, 12);
    root.translatesAutoresizingMaskIntoConstraints = NO;
    NSView *content = win.contentView;
    [content addSubview:root];
    [NSLayoutConstraint activateConstraints:@[
        [root.leadingAnchor constraintEqualToAnchor:content.leadingAnchor],
        [root.trailingAnchor constraintEqualToAnchor:content.trailingAnchor],
        [root.topAnchor constraintEqualToAnchor:content.topAnchor],
        [root.bottomAnchor constraintEqualToAnchor:content.bottomAnchor],
    ]];

}

static void buildMenus(void) {
    NSMenu *bar = [NSMenu new];
    NSMenuItem *appItem = [NSMenuItem new];
    [bar addItem:appItem];
    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Hyper Stack"];
    [appMenu addItemWithTitle:@"Hide Hyper Stack" action:@selector(hide:) keyEquivalent:@"h"];
    [appMenu addItem:[NSMenuItem separatorItem]];
    [appMenu addItemWithTitle:@"Quit Hyper Stack" action:@selector(terminate:) keyEquivalent:@"q"];
    appItem.submenu = appMenu;

    NSMenuItem *editItem = [NSMenuItem new];
    [bar addItem:editItem];
    NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
    [edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    editItem.submenu = edit;

    NSMenuItem *winItem = [NSMenuItem new];
    [bar addItem:winItem];
    NSMenu *wm = [[NSMenu alloc] initWithTitle:@"Window"];
    [wm addItemWithTitle:@"Close" action:@selector(performClose:) keyEquivalent:@"w"];
    [wm addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
    winItem.submenu = wm;
    [NSApp setMainMenu:bar];
    [NSApp setWindowsMenu:wm];
}

static void buildStatusItem(void) {
    statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    statusItem.button.toolTip = @"Hyper Stack";
    if (statusItem.button.image == nil) statusItem.button.title = @"H";
    NSMenu *m = [NSMenu new];
    NSArray *items = @[@[@"Open Hyper Stack", @"show"], @[@"Open in the browser", @"web"], @[@"Stop all services", @"stopall"]];
    for (NSArray *it in items) {
        NSMenuItem *mi = [[NSMenuItem alloc] initWithTitle:it[0] action:@selector(menuItem:) keyEquivalent:@""];
        mi.target = ctl;
        mi.representedObject = it[1];
        [m addItem:mi];
    }
    [m addItem:[NSMenuItem separatorItem]];
    NSMenuItem *q = [[NSMenuItem alloc] initWithTitle:@"Quit" action:@selector(menuItem:) keyEquivalent:@""];
    q.target = ctl;
    q.representedObject = @"quit";
    [m addItem:q];
    statusItem.menu = m;
}

// ---------------------------------------------------------------- public API

void hs_run(int showAtStart) {
    @autoreleasepool {
        [[NSProcessInfo processInfo] setProcessName:@"Hyper Stack"];
        [NSApplication sharedApplication];
        ctl = [HSCtl new];
        NSApp.delegate = ctl;
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        buildMenus();
        buildWindow();
        buildStatusItem();
        [NSApp finishLaunching];
        if (showAtStart) hs_show();
        [NSApp run];
    }
}

void hs_quit(void) {
    later(^{
        quitting = YES;
        [NSApp stop:nil];
        NSEvent *e = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0
                                    windowNumber:0 context:nil subtype:0 data1:0 data2:0];
        [NSApp postEvent:e atStart:YES];
    });
}

void hs_show(void) {
    later(^{
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        [win makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    });
}

void hs_set_icon(const void *png, int len) {
    NSData *d = [NSData dataWithBytes:png length:(NSUInteger)len];
    later(^{
        NSImage *img = [[NSImage alloc] initWithData:d];
        if (!img) return;
        [NSApp setApplicationIconImage:img];
        NSImage *small = [img copy];
        small.size = NSMakeSize(18, 18);
        statusItem.button.image = small;
        statusItem.button.title = @"";
    });
}

static void fillPopup(NSPopUpButton *p, NSArray<NSString *> *items, NSArray<NSString *> *__strong *shown) {
    if (*shown && [*shown isEqualToArray:items]) return;
    NSString *keep = p.titleOfSelectedItem;
    [p removeAllItems];
    [p addItemsWithTitles:items];
    if (keep && [items containsObject:keep]) [p selectItemWithTitle:keep];
    *shown = items;
}

void hs_update(const char *json) {
    NSData *d = [S(json) dataUsingEncoding:NSUTF8StringEncoding];
    later(^{
        NSDictionary *o = [NSJSONSerialization JSONObjectWithData:d options:0 error:nil];
        if (![o isKindOfClass:[NSDictionary class]]) return;
        statusLabel.stringValue = o[@"status"] ?: @"";
        buttons[@"install"].hidden = ![o[@"installVisible"] boolValue];
        BOOL all = [o[@"allEnabled"] boolValue];
        buttons[@"startall"].enabled = all;
        buttons[@"stopall"].enabled = all;
        loginCheck.state = [o[@"loginOn"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
        settingsText.string = o[@"settings"] ?: @"";
        fillPopup(phpPop, o[@"php"] ?: @[], &shownPHP);
        NSArray *logs = o[@"logs"] ?: @[];
        BOOL first = shownLogs == nil || shownLogs.count == 0;
        fillPopup(logPop, logs, &shownLogs);
        if (first && logs.count > 0) act(@"logfile", logs[0]);

        NSMutableArray *names = [NSMutableArray array], *rows = [NSMutableArray array];
        for (NSDictionary *s in o[@"sites"] ?: @[]) {
            [names addObject:s[@"name"] ?: @""];
            [rows addObject:s[@"cells"] ?: @[]];
        }
        siteNames = names;
        siteData.rows = rows;
        NSString *want = selectedName;
        [siteTable reloadData];
        NSInteger idx = want ? (NSInteger)[names indexOfObject:want] : NSNotFound;
        if (idx == NSNotFound) idx = names.count > 0 ? 0 : -1;
        if (idx >= 0) {
            [siteTable selectRowIndexes:[NSIndexSet indexSetWithIndex:(NSUInteger)idx] byExtendingSelection:NO];
        }
        svcData.rows = o[@"services"] ?: @[];
        [svcTable reloadData];
    });
}

void hs_set_selection(const char *json) {
    NSData *d = [S(json) dataUsingEncoding:NSUTF8StringEncoding];
    later(^{
        NSDictionary *o = [NSJSONSerialization JSONObjectWithData:d options:0 error:nil];
        if (![o isKindOfClass:[NSDictionary class]]) return;
        for (NSString *k in @[@"start", @"stop", @"open", @"pma", @"import", @"folder", @"switchphp", @"forget"]) {
            buttons[k].enabled = [o[k] boolValue];
        }
        siteAuto.enabled = [o[@"siteauto"] boolValue];
        phpPop.enabled = [o[@"switchphp"] boolValue];
        siteAuto.state = [o[@"siteautoOn"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
        NSNumber *pi = o[@"phpIndex"];
        if (pi && [pi integerValue] >= 0 && [pi integerValue] < phpPop.numberOfItems) [phpPop selectItemAtIndex:[pi integerValue]];
    });
}

void hs_set_job(const char *title, const char *log) {
    NSString *t = S(title), *l = S(log);
    later(^{
        jobLabel.stringValue = t;
        if (![jobLog.string isEqualToString:l]) {
            jobLog.string = l;
            [jobLog scrollRangeToVisible:NSMakeRange(l.length, 0)];
        }
    });
}

void hs_set_text(int which, const char *text) {
    NSString *t = S(text);
    later(^{
        NSTextView *tv = which == 0 ? healthText : logText;
        tv.string = t;
        [tv scrollRangeToVisible:NSMakeRange(t.length, 0)];
    });
}

// ---------------------------------------------------------------- dialogs

int hs_alert(const char *title, const char *msg, int warn, const char *b1, const char *b2, const char *b3) {
    __block int result = 0;
    NSString *t = S(title), *m = S(msg), *s1 = S(b1), *s2 = b2 ? S(b2) : nil, *s3 = b3 ? S(b3) : nil;
    onMain(^{
        NSAlert *a = [NSAlert new];
        a.messageText = t;
        a.informativeText = m;
        a.alertStyle = warn ? NSAlertStyleWarning : NSAlertStyleInformational;
        [a addButtonWithTitle:s1];
        if (s2) [a addButtonWithTitle:s2];
        if (s3) [a addButtonWithTitle:s3];
        [NSApp activateIgnoringOtherApps:YES];
        result = (int)([a runModal] - NSAlertFirstButtonReturn);
    });
    return result;
}

static char *panel(NSString *title, BOOL folders) {
    __block char *out = NULL;
    onMain(^{
        NSOpenPanel *p = [NSOpenPanel openPanel];
        p.title = title;
        p.canChooseFiles = !folders;
        p.canChooseDirectories = folders;
        p.canCreateDirectories = folders;
        p.allowsMultipleSelection = NO;
        [NSApp activateIgnoringOtherApps:YES];
        if ([p runModal] == NSModalResponseOK && p.URL.path) out = strdup(p.URL.path.UTF8String);
    });
    return out;
}

char *hs_open_file(const char *title) { return panel(S(title), NO); }
char *hs_open_folder(const char *title) { return panel(S(title), YES); }

@interface HSBrowse : NSObject
@property(strong) NSTextField *field;
- (void)browse:(id)sender;
@end
@implementation HSBrowse
- (void)browse:(id)sender {
    NSOpenPanel *p = [NSOpenPanel openPanel];
    p.title = @"Choose the install folder";
    p.canChooseFiles = NO;
    p.canChooseDirectories = YES;
    p.canCreateDirectories = YES;
    if ([p runModal] == NSModalResponseOK && p.URL.path) self.field.stringValue = p.URL.path;
}
@end

int hs_install_dialog(const char *dir, int dirEditable, int httpPort, int httpsPort, int mysqlPort, char **outDir, int *outPorts) {
    __block int accepted = 0;
    NSString *d0 = S(dir);
    onMain(^{
        NSAlert *a = [NSAlert new];
        a.messageText = @"Install Hyper Stack";
        a.informativeText = @"Choose one folder. Everything goes inside it, with a www folder for your projects. "
                             "It downloads about 400 MB and asks for your password once, to trust the local certificate and edit the hosts file.";
        NSView *acc = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 460, 96)];
        NSTextField *df = [NSTextField textFieldWithString:d0];
        df.frame = NSMakeRect(0, 66, 360, 24);
        df.editable = dirEditable != 0;
        NSButton *br = [NSButton buttonWithTitle:@"Browse..." target:nil action:nil];
        br.frame = NSMakeRect(366, 64, 94, 28);
        br.enabled = dirEditable != 0;
        HSBrowse *bh = [HSBrowse new];
        bh.field = df;
        br.target = bh;
        br.action = @selector(browse:);
        [acc addSubview:df];
        [acc addSubview:br];
        NSArray *names = @[@"http", @"https", @"mysql"];
        int vals[3] = {httpPort, httpsPort, mysqlPort};
        NSMutableArray<NSTextField *> *fields = [NSMutableArray array];
        for (int i = 0; i < 3; i++) {
            NSTextField *l = [NSTextField labelWithString:names[i]];
            l.frame = NSMakeRect(i * 150, 30, 50, 20);
            NSTextField *f = [NSTextField textFieldWithString:[NSString stringWithFormat:@"%d", vals[i]]];
            f.frame = NSMakeRect(i * 150 + 52, 28, 80, 24);
            [acc addSubview:l];
            [acc addSubview:f];
            [fields addObject:f];
        }
        NSTextField *note = [NSTextField labelWithString:@"Change the ports only if Laragon or XAMPP is running."];
        note.frame = NSMakeRect(0, 2, 460, 18);
        [acc addSubview:note];
        a.accessoryView = acc;
        [a addButtonWithTitle:@"Install"];
        [a addButtonWithTitle:@"Cancel"];
        [NSApp activateIgnoringOtherApps:YES];
        if ([a runModal] == NSAlertFirstButtonReturn) {
            accepted = 1;
            *outDir = strdup(df.stringValue.UTF8String);
            for (int i = 0; i < 3; i++) outPorts[i] = fields[i].intValue;
        }
    });
    return accepted;
}
