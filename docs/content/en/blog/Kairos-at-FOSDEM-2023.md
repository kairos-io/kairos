---
title: "Kairos at FOSDEM 2023"
date: 2023-02-07T10:53:13+01:00
draft: true
author: Mauro Morales ([Twitter](https://twitter.com/mauromrls)) ([GitHub](https://github.com/mauromorales))
---

I recently had the opportunity to attend FOSDEM 2023 and share a bit about the Kairos project. In this post I want to share a bit about the talk and other interesting presentations, which I found quite interesting and I believe are relevant for Kairos and our community.

## How we build and maintain Kairos

First of all, if you're here. You might want to check my talk about [How we build and maintain Kairos](https://fosdem.org/2023/schedule/event/kairos/). In first half of the presentation, I introduce the different elements that make Kairos a great OS for Edge Kubernetes. Because my presentation took place in the Distributions Devroom, I put some emphasis on the challenges we have to be distribution agnostic. During the second half of the presentation you will get an overview of how the Kairos Factory works, starting from those different Linux distributions all the way up to producing Kairos core and standard images.

The talk is intended to newcomers, so I made an effort to describe things in a simple and welcoming language. However, I think it can also be interesting for those who might already know about Kairos but wonder how to extend the core and standard images, or simply have a better understanding of how all the pieces interconnect.

Like I said, the presentation took place in the Distributions Devroom and we're very thankful to them for hosting us. While it was a great experience and the talk seemed to have a good reception, I now realize that the topic is probably more relevant for a different in other devrooms, for example, the [
Image-based Linux and Secure Measured Boot devroom
](https://fosdem.org/2023/schedule/track/image_based_linux_and_secure_measured_boot/), which I'll make sure to send proposals next year.

## Other talks which are relevant to Kairos

There were other interesting presentations I had the opportunity to attend, which I think are also relevant to Kairos and our community. These would be my top pics:

If you're completely new to the concepts of Image-Based Linux, Unified Kernel Image or Discoverable Disk Image, I'd recommend checking Luca Bocassi's talk [Introducing and decoding image-based Linux terminology and concepts](https://fosdem.org/2023/schedule/event/image_linux_secureboot_uki_ddi_ohmy/). As someone who very recently joined the Kairos project, I still get a bit lost with all the different technologies used in Image-Based Linux. The presenter made a good job clarifying some of these technologies and how they work together.

One of the key presentations in my opinion was Lennart Poettering presentation, [Measured Boot, Protecting Secrets and you](https://fosdem.org/2023/schedule/event/image_linux_secureboot_tpm/), where he talks about Trusted Plataform Modules and upcoming functionality in Systemd. I'm pretty sure there will be some of these features which will be relevant for Kairos sooner rather than later.

Last but not least, there was an interesting talk by Gabriel Kerneis about [User-friendly Lightweight TPM Remote Attestation over Bluetooth](https://fosdem.org/2023/schedule/event/image_linux_secureboot_ultrablue/). My guess is that we will continue seeing different methods to do and simplify attestation and because one of our goals at the Kairos project is to be as friendly as we can to our user base, then I can only imagine we will end up introducing some sort of remote attestation technologies like Ultrablue in the future.

## Conclusion

FOSDEM is a very important conference when it comes to free and open source software and I'm very happy that Kairos was present. First of all because I think the work we're doing with Kairos is helping solve some of the most challenging issues of running cloud native applications on the edge, but also because as an open source project, it was nice to introduce ourselves to the community there and start a conversation. Expect us to keep engaging with you in further editions of FOSDEM and other conferences!
