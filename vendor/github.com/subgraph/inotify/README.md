This repository is a fork of the unmaintained/deprecated `golang.org/x/exp/inotify`
library. This code was mirrored for a time on Github but is now **deleted** 
from the mirror.

As we and a number of other people still use the library, we thought it would
be best to fork it.

How we created the fork:

1. Cloned the following mirror:
```
git clone https://github.com/golang/exp
```

2. Reverted back to the `inotify: fix memory leak in Watcher` commit:
```
git reset --hard 292a51b8d262487dab23a588950e8052d63d9113
```

3. Copied the `inotify` directory and licensing info into a new git repo

This was not our favorite way to fork `inotify` but we didn't have many other
choices. We chose to fork this perfectly good (but deprecated) library
that fulfills our needs because the newer one (`fsnotify`) simply does not.

For more information about how we came to the decision to fork this library,
see the following issue:
[Need fully functional inotify golang package](https://github.com/subgraph/subgraph-os-issues/issues/230)



