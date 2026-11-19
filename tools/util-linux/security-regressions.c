/* Exercise the compiled private mount implementation without mounting anything.
 * The mount(2) boundary is replaced with an inode-checking EPERM stub. All paths
 * and mutations are confined to the caller's disposable fixture directory. */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include "mountP.h"
#include "fileutils.h"
#include <sys/stat.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define hookset_mount_legacy artex_test_hookset_mount_legacy
extern const struct libmnt_hookset artex_test_hookset_mount_legacy;
#include "libmount/src/hook_mount_legacy.c"
#undef hookset_mount_legacy

static int calls, pinned;
static ino_t source_inode, target_inode;
static int mount_post, final_post, result_status, result_exec;
static const char *fixture_target;

int mount(const char *src, const char *target, const char *type,
          unsigned long flags, const void *data)
{
    struct stat source, destination;
    (void) type; (void) data;
    calls++;
    pinned = !strncmp(src, "/proc/self/fd/", 14)
          && !strncmp(target, "/proc/self/fd/", 14)
          && (flags & MS_BIND)
          && stat(src, &source) == 0 && stat(target, &destination) == 0
          && source.st_ino == (ino_t) source_inode
          && destination.st_ino == (ino_t) target_inode;
    errno = EPERM;
    return -1;
}

static void require(int condition, const char *message)
{
    if (!condition) { fprintf(stderr, "security regression: %s\n", message); exit(1); }
}

static int simulate_helper(struct libmnt_context *cxt,
                           const struct libmnt_hookset *hs, void *data)
{
    (void) hs; (void) data;
    cxt->helper_exec_status = result_exec;
    cxt->helper_status = result_status;
    return result_exec < 0 ? result_exec : 0;
}

static int post_mount(struct libmnt_context *cxt,
                      const struct libmnt_hookset *hs, void *data)
{
    (void) cxt; (void) hs; (void) data;
    mount_post++;
    return chmod(fixture_target, 0700);
}

static int post_final(struct libmnt_context *cxt,
                      const struct libmnt_hookset *hs, void *data)
{
    (void) cxt; (void) hs; (void) data;
    final_post++;
    return chmod(fixture_target, 0700);
}

/* Attach synthetic callbacks to a registered slot so the real mount()
 * lifecycle can deinitialize them before returning. */
static const struct libmnt_hookset *synthetic = &hookset_owner;

static void check_helper(int status, int exec_status, int positive)
{
    struct libmnt_context *cxt = mnt_new_context();
    struct stat st;
    require(cxt != NULL, "allocate context");
    require(chmod(fixture_target, 0755) == 0, "reset target mode");
    mnt_context_set_source(cxt, "none");
    mnt_context_set_target(cxt, fixture_target);
    mnt_context_set_fstype(cxt, "artex-fixture");
    mnt_context_disable_mtab(cxt, 1);
    cxt->action = MNT_ACT_MOUNT;
    cxt->flags |= MNT_FL_PREPARED | MNT_FL_MOUNTFLAGS_MERGED;
    result_status = status; result_exec = exec_status;
    mount_post = final_post = 0;
    require(mnt_context_append_hook(cxt, synthetic, MNT_STAGE_MOUNT, NULL, simulate_helper) == 0, "mount callback");
    require(mnt_context_append_hook(cxt, synthetic, MNT_STAGE_MOUNT_POST, NULL, post_mount) == 0, "mount post callback");
    require(mnt_context_append_hook(cxt, synthetic, MNT_STAGE_POST, NULL, post_final) == 0, "final post callback");
    (void) mnt_context_mount(cxt);
    require(stat(fixture_target, &st) == 0, "stat target after failed helper");
    require(mount_post == positive && final_post == positive, "failed helper reached a privileged post hook");
    require((st.st_mode & 0777) == (positive ? 0700 : 0755), "failed helper changed pre-existing inode");
    mnt_free_context(cxt);
}

static void check_legacy(const char *src, const char *target, int should_call)
{
    struct libmnt_context *cxt = mnt_new_context();
    require(cxt != NULL, "allocate legacy context");
    require(mnt_context_is_restricted(cxt), "fixture must run as a non-root user");
    mnt_context_set_source(cxt, src);
    mnt_context_set_target(cxt, target);
    mnt_context_set_options(cxt, "bind");
    calls = pinned = 0;
    require(hook_mount(cxt, &artex_test_hookset_mount_legacy, NULL) != 0, "no real mount may succeed");
    require(calls == should_call, "symlink source/target reached mount(2)");
    require(!should_call || pinned, "legacy bind did not use pinned source and target inode FDs");
    mnt_free_context(cxt);
}

int main(int argc, char **argv)
{
    char src[4096], target[4096], link[4096], nested[4096];
    struct stat st;
    require(argc == 2 && getuid() != 0, "private directory and non-root UID required");
    snprintf(src, sizeof(src), "%s/source", argv[1]);
    snprintf(target, sizeof(target), "%s/target", argv[1]);
    snprintf(link, sizeof(link), "%s/redirect", argv[1]);
    snprintf(nested, sizeof(nested), "%s/redirect/child", argv[1]);
    if (mkdir(src, 0755) != 0) {
        int error = errno;
        struct stat parent;
        if (stat(argv[1], &parent) == 0)
            fprintf(stderr, "fixture parent uid=%lu gid=%lu mode=%o process=%lu/%lu/%lu mkdir=%s\n",
                    (unsigned long) parent.st_uid, (unsigned long) parent.st_gid,
                    parent.st_mode & 0777, (unsigned long) getuid(),
                    (unsigned long) geteuid(), (unsigned long) getgid(), strerror(error));
        require(0, "create fixture source directory");
    }
    require(mkdir(target, 0755) == 0, "create fixture target directory");
    require(symlink(src, link) == 0, "create adversarial source symlink");
    { char *child = NULL;
      require(asprintf(&child, "%s/child", src) > 0, "compose nested source");
      require(mkdir(child, 0755) == 0, "create nested source");
      free(child); }
    fixture_target = target;
    check_helper(1, 0, 0);
    check_helper(127, 0, 0);
    check_helper(9, 0, 0);
    check_helper(0, -ENOENT, 0);
    check_helper(0, 0, 1);
    require(stat(src, &st) == 0, "source inode"); source_inode = st.st_ino;
    require(stat(target, &st) == 0, "target inode"); target_inode = st.st_ino;
    check_legacy(src, target, 1);
    check_legacy(link, target, 0);
    check_legacy(nested, target, 0);
    check_legacy(src, link, 0);
    puts("mount security regressions: 9 passed (no real mount syscalls)");
    return 0;
}
