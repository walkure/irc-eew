package EEWSock;

# EEWSock.pm
# walkure at 3pf.jp

use strict;
use warnings;
use base qw/IO::Socket::INET/;

use Encode;
use Data::Dumper;
use HTTP::Tiny;
use HTTP::Headers;
use DateTime;
use Digest::MD5 qw(md5_hex);

sub configure
{
	my ( $self, $args ) = @_;
	
	*$self->{server_list} = $args->{ServerList} if defined $args->{ServerList};
	*$self->{callback} = $args->{Callback} if defined $args->{Callback};
	
	*$self->{header} = {};
	*$self->{buffer} = '';
	
	$self->get_server_list() unless defined *$self->{server_list};
	
	my ($host,$port) = $self->choose_server();
	print "+Choose $host:$port\n";
	
	$args->{PeerAddr} = "$host:$port";
	
	$self->SUPER::configure($args);
}

sub choose_server
{
	my $self = shift;
	my @server = @{*$self->{server_list}};
	my $sum = scalar @server;
	my ( $ip, $port ) = split /:/, $server[ int( rand($sum) ) ];
	
	($ip,$port);
}

sub get_server_list
{
	my $self = shift;
	
	if(ref($self) eq 'EEWSock' && defined *$self->{server_list}){
		print 'Cached '.scalar @{*$self->{server_list}}." servers\n";
		return *$self->{server_list};
	}
	
	my $ua = HTTP::Tiny->new(agent => 'FastCaster/1.0 powered by weathernews.');
	my @server;
	
	my $res = $ua->get('http://lst10s-sp.wni.co.jp/server_list.txt');
	if ( $res->{success}) {
		@server = split /[\r\n]+/, $res->{content};
	}
	else {
	    die "Cannot get server list: $res->{status} $res->{reason} \n";
	}
	print 'Retrieved '.scalar @server." servers\n";
	
	if(ref($self) eq 'EEWSock'){
		*$self->{server_list} = \@server;
	}else{
		\@server;
	}
}

sub wni_timestamp
{
	my $dt = DateTime->now;
	$dt->strftime('%Y/%m/%d %H:%M:%S.%6N');
}

sub send_ack
{
	my $self = shift;
	
	my $now = wni_timestamp();
	my $response = HTTP::Headers->new(
		'Content-Type' => 'application/fast-cast',
		'Server' => 'FastCaster/1.0.0 (Unix)',
		'X-WNI-ID' => 'Response',
		'X-WNI-Result' => 'OK',
		'X-WNI-Protocol-Version' => '2.1',
		'X-WNI-Time' => $now
	);
	print $self "HTTP/1.0 200 OK\n";
	print $self $response->as_string;
	print $self "\n";

	print scalar localtime." Send Request(Timeout) Waiting...\n";
	$self->flush();
}

sub logon
{
	my ($self,$conf) = @_;
	
	my $hash = defined $conf->{'passwd-md5'} ? $conf->{'passwd-md5'} : md5_hex($conf->{passwd});

	my $now = wni_timestamp();
	my $request = HTTP::Headers->new(
		'User-Agent' => 'FastCaster/1.0 powered by weathernews.',
		'Accept' => '*/*',
		'Cache-Control' => 'no-cache',
		'X-WNI-Account' => $conf->{user},
		'X-WNI-Password' => $hash,
		'X-WNI-Application-Version' => '2.2.4.0',
		'X-WNI-Authentication-Method' => 'MDB_MWS',
		'X-WNI-ID' => 'Login',
		'X-WNI-Protocol-Version' => '2.1',
		'X-WNI-Terminal-ID' => '211363088',
		'X-WNI-Time' => $now
	);
	print $self "GET /login HTTP/1.0\n";
	print $self $request->as_string ;
	print $self "\n";
	
	$self->flush();
	print scalar localtime." Send Request waiting...\n";
}

sub parse_body
{
	my ($self,$body) = @_;
	
	*$self->{buffer} .= $body;
	
	unless(defined *$self->{size}){
		my $i;
		while (($i = index(*$self->{buffer},"\x0a")) >= 0){ #index for the head of string is 0
			my $line = substr(*$self->{buffer},0,$i);
			*$self->{buffer} = substr(*$self->{buffer},$i+1);
			if($line =~ /GET \/ HTTP\/1.1/){
				$self->send_ack();
				*$self->{header} = {};
				*$self->{buffer} = '';
				next;
			}
			if($line =~ /:/){
				my($name,$body) = split(/:\s*/,$line);
				*$self->{header}{$name} = $body;
				next;
			}
			
			unless(length $line){
				my $id = *$self->{header}{'X-WNI-ID'};
				if($id eq 'Data'){
					if(defined *$self->{header}{'Content-Length'}){
						print 'Begin Contents('.*$self->{header}{'Content-Length'}.")\n";
						*$self->{size} = *$self->{header}{'Content-Length'} + 0;
						#break from while
						last;
					}
				}elsif($id eq 'Response'){
					print 'Auth Status:'.*$self->{header}{'X-WNI-Result'}."\n";
				}elsif($id eq 'Keep-Alive'){
					print scalar localtime." Keep-Alive...\n";
				}else{
					print "STATE:[$id]\n";
				}
				*$self->{size} = undef;
			}
		}
	}
	
	if(defined *$self->{size}){
		if(*$self->{size} == length *$self->{buffer}){
			if(defined *$self->{callback}){
				*$self->{callback}->($self,*$self->{header}{'X-WNI-Data-MD5'},*$self->{buffer});
			}
			*$self->{size} = undef;
			*$self->{buffer} = '';
		}
	}
}

sub set_callback
{
	my($self,$func) = @_;
	*$self->{callback} = $func;
}


1;

