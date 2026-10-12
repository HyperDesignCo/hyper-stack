#ifndef HYPER_COCOA_H
#define HYPER_COCOA_H

// The Hyper Stack window on macOS (AppKit). Every setter may be called from any
// thread; the work is done on the main thread. User actions are reported through
// goAction (implemented in Go).

void hs_run(int showAtStart);          // builds the UI and blocks in the main run loop
void hs_quit(void);                    // makes hs_run return
void hs_show(void);                    // bring the window to the front
void hs_set_icon(const void *png, int len);
void hs_update(const char *json);      // the whole state, see mac_darwin.go
void hs_set_selection(const char *json);
void hs_set_job(const char *title, const char *log);
void hs_set_text(int which, const char *text); // 0 health, 1 log file

int hs_alert(const char *title, const char *msg, int warn, const char *b1, const char *b2, const char *b3);
char *hs_open_file(const char *title);   // malloc'd path or NULL
char *hs_open_folder(const char *title); // malloc'd path or NULL
int hs_install_dialog(const char *dir, int dirEditable, int httpPort, int httpsPort, int mysqlPort,
                      char **outDir, int *outPorts);

#endif
