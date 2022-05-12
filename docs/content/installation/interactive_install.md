+++
title = "Interactive installation"
date = 2022-02-09T17:56:26+01:00
weight = 2
chapter = false
pre = "<b>- </b>"
+++

## Start the c3os ISO

Download and mount the ISO in either baremetal or a VM that you wish to use as a node for your cluster.

It doesn't matter if you are joining a node to an existing cluster or creating a new one, the procedure is still the same.

A GRUB menu will be displayed:

![Screenshot from 2022-05-12 22-29-31](https://user-images.githubusercontent.com/2420543/168162623-b3a62107-c32c-4ac8-b484-a429b56c1626.png)

Select the third entry (`c3os (interactive install)`).

![Screenshot from 2022-05-12 22-30-07](https://user-images.githubusercontent.com/2420543/168162681-26597b53-afe6-4df8-8b9d-0f61e4a535a6.png)

A boot splash screen will appear, and right after a prompt, asking for installation settings:
![Screenshot from 2022-05-12 22-32-54](https://user-images.githubusercontent.com/2420543/168163058-83dda8cc-28f2-4d12-bfde-2a4cc556b82b.png)

After entering all the details, the installation will start, returning finally at the shell.

{{% notice note %}}
The interactive installer can be also started manually with `c3os interactive-install` from the LiveCD.
{{% /notice %}}