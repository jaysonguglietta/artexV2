/* Test-only kernel boundary simulation: no namespace transition and no cgroup
 * write occur. Verify the real compiled nsenter closes its temporary authority
 * descriptor before the credential change, as well as marking it CLOEXEC. */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <sys/syscall.h>
#include <unistd.h>

static int authority_fd = -1, saw_authority, had_cloexec, saw_uid_drop;

static int open_fixture(const char *path, int flags, mode_t mode)
{
    const char *fixture = getenv("ARTEX_CGROUP_FIXTURE");
    if (fixture && !strncmp(path, "/sys/fs/cgroup/", 15)
        && strstr(path, "/cgroup.procs")) {
        path = fixture;
        saw_authority = 1;
        had_cloexec = !!(flags & O_CLOEXEC);
        authority_fd = syscall(SYS_openat, AT_FDCWD, path, flags, mode);
        return authority_fd;
    }
    return syscall(SYS_openat, AT_FDCWD, path, flags, mode);
}

int open(const char *path, int flags, ...)
{
    mode_t mode = 0;
    if (flags & O_CREAT) { va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap); }
    return open_fixture(path, flags, mode);
}
int open64(const char *path, int flags, ...)
{
    mode_t mode = 0;
    if (flags & O_CREAT) { va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap); }
    return open_fixture(path, flags, mode);
}
int setns(int fd, int type) { (void) fd; (void) type; return 0; }
int setuid(uid_t uid)
{
    if (!saw_authority || !had_cloexec || authority_fd < 0 || fcntl(authority_fd, F_GETFD) >= 0)
        _exit(91);
    saw_uid_drop = 1;
    return syscall(SYS_setuid, uid);
}
int execvp(const char *file, char *const argv[])
{
    int (*real_execvp)(const char *, char *const []) = dlsym(RTLD_NEXT, "execvp");
    if (!saw_uid_drop || !saw_authority || !had_cloexec || fcntl(authority_fd, F_GETFD) >= 0)
        _exit(92);
    return real_execvp(file, argv);
}
