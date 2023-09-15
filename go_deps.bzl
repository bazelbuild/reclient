load("@bazel_gazelle//:deps.bzl", "go_repository")

def shirou_gopsutil_deps():
    # this repo is the dependency for com_github_shirou_gopsutil Mac OS build.
    go_repository(
        name = "com_github_shoenig_go-m1cpu",
        commit = "41fe74c064b56dad60b3cbe1a62f82d39a06960b",
        importpath = "github.com/shoenig/go-m1cpu",
    )

    # this repo is the dependency for com_github_shirou_gopsutil windows build.
    go_repository(
        name = "com_github_go_ole_go_ole",
        importpath = "github.com/go-ole/go-ole",
        sum = "h1:/Fpf6oFPoeFik9ty7siob0G6Ke8QvQEuVcuChpwXzpY=",
        version = "v1.2.6",
    )

    # this repo is the dependency for com_github_shirou_gopsutil windows build.
    go_repository(
        name = "com_github_yusufpapurcu_wmi",
        commit = "84686519bfe3928447925505e8201e997c0ad0c1",
        importpath = "github.com/yusufpapurcu/wmi",
    )

    # this repo is the dependency for com_github_shirou_gopsutil.
    go_repository(
        name = "com_github_tklauser_go-sysconf",
        commit = "8725eefab62068f7b2e637e6e3de89682ae5052b",
        importpath = "github.com/tklauser/go-sysconf",
    )

    #this repo is the dependency for com_github_shirou_gopsutil.
    go_repository(
        name = "com_github_tklauser_numcpus",
        commit = "7db9889716ca99cfb0278267fb969161af9bb03d",
        importpath = "github.com/tklauser/numcpus",
    )
